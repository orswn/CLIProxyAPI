package management

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/bedrockmantle"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

const mantleProvider = bedrockmantle.ProviderType

// RequestBedrockMantleToken starts the IAM Identity Center device flow.
//
// Query: start_url, sso_region, prefix, region, role_arn, or auth_file for reauthentication.
// Poll /get-auth-status for completion and /bedrock-mantle-choices for account/role selection.
func (h *Handler) RequestBedrockMantleToken(c *gin.Context) {
	ctx := PopulateAuthContext(context.Background(), c)

	startURL := strings.TrimSpace(c.Query("start_url"))
	storage := &bedrockmantle.Storage{
		AuthMode:      bedrockmantle.ModeSSO,
		StartURL:      startURL,
		SSORegion:     strings.TrimSpace(c.Query("sso_region")),
		Prefix:        strings.Trim(strings.TrimSpace(c.Query("prefix")), "/"),
		DefaultRegion: strings.TrimSpace(c.Query("region")),
		AccountID:     strings.TrimSpace(c.Query("account_id")),
		RoleName:      strings.TrimSpace(c.Query("role_name")),
		RoleARN:       strings.TrimSpace(c.Query("role_arn")),
	}
	var original *coreauth.Auth
	if name := strings.TrimSpace(c.Query("auth_file")); name != "" {
		auth, ok := h.lookupAuthFile(name, "")
		if !ok || auth == nil || auth.Provider != mantleProvider {
			c.JSON(http.StatusNotFound, gin.H{"error": "Bedrock Mantle credential not found"})
			return
		}
		original = auth.Clone()
		storage = bedrockmantle.FromMetadata(original.Metadata)
		if storage == nil || storage.AuthMode != bedrockmantle.ModeSSO {
			c.JSON(http.StatusBadRequest, gin.H{"error": "credential does not use IAM Identity Center"})
			return
		}
		storage.AccessToken = ""
		storage.RefreshToken = ""
	}
	startURL = storage.StartURL
	if startURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start_url is required"})
		return
	}
	if storage.SSORegion == "" {
		storage.SSORegion = bedrockmantle.DefaultSSORegion
	}
	if original == nil {
		if err := applyMantleSetupRegions(storage, c.Query("model_regions")); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	client := bedrockmantle.NewClient(storage.SSORegion, nil)
	da, err := client.StartDeviceAuthorization(ctx, startURL)
	if err != nil {
		log.Errorf("Failed to start Bedrock Mantle device flow: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to start device authorization flow"})
		return
	}

	random := make([]byte, 24)
	if _, errRandom := rand.Read(random); errRandom != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create login session"})
		return
	}
	state := "mantle-" + hex.EncodeToString(random)
	RegisterOAuthSession(state, mantleProvider)
	pending := &mantlePendingSession{storage: storage, client: client, original: original}
	mantleSessions.put(state, pending)

	go func() {
		pollCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go watchOAuthSessionCancel(pollCtx, cancel, state, mantleProvider)

		tok, errWait := client.WaitForToken(pollCtx, da)
		if errWait != nil {
			if !IsOAuthSessionPending(state, mantleProvider) {
				mantleSessions.del(state)
				return
			}
			SetOAuthSessionError(state, oauthSessionErrorWithCause("Authentication failed", errWait))
			mantleSessions.del(state)
			return
		}
		if !IsOAuthSessionPending(state, mantleProvider) {
			mantleSessions.del(state)
			return
		}
		pending.mu.Lock()
		storage.ApplySession(&bedrockmantle.Session{
			StartURL:              storage.StartURL,
			SSORegion:             storage.SSORegion,
			ClientID:              da.ClientID,
			ClientSecret:          da.ClientSecret,
			ClientSecretExpiresAt: da.ClientSecretExpiresAt,
			Token:                 *tok,
		})
		pending.mu.Unlock()
		h.advanceMantleSession(pollCtx, state)
	}()

	response := gin.H{"status": "ok", "url": da.VerificationURIComplete, "state": state, "flow": "device", "user_code": da.UserCode}
	if response["url"] == "" {
		response["url"] = da.VerificationURI
	}
	if !da.ExpiresAt.IsZero() {
		response["expires_in"] = int(time.Until(da.ExpiresAt) / time.Second)
	}
	c.JSON(http.StatusOK, response)
}

// SelectBedrockMantleTarget receives the account/role the user picked.
// Form body: state and one available account_id or role_name.
func (h *Handler) SelectBedrockMantleTarget(c *gin.Context) {
	state := strings.TrimSpace(c.Query("state"))
	if state == "" {
		state = strings.TrimSpace(c.PostForm("state"))
	}
	sess := mantleSessions.get(state)
	if sess == nil || !IsOAuthSessionPending(state, mantleProvider) {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown or expired state"})
		return
	}
	sess.mu.Lock()
	valid := applyMantleSelection(sess, strings.TrimSpace(c.PostForm("account_id")), strings.TrimSpace(c.PostForm("role_name")))
	sess.mu.Unlock()
	if !valid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "select an available account or role after device approval"})
		return
	}
	h.advanceMantleSession(PopulateAuthContext(context.Background(), c), state)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// GetBedrockMantleChoices returns pending account/role choices for a state.
func (h *Handler) GetBedrockMantleChoices(c *gin.Context) {
	state := strings.TrimSpace(c.Query("state"))
	sess := mantleSessions.get(state)
	if sess == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown or expired state"})
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	out := gin.H{"status": sess.status, "account_id": sess.storage.AccountID, "role_name": sess.storage.RoleName}
	if len(sess.accounts) > 0 {
		accounts := make([]gin.H, 0, len(sess.accounts))
		for _, account := range sess.accounts {
			accounts = append(accounts, gin.H{"id": account.ID, "name": account.Name})
		}
		out["accounts"] = accounts
	}
	if len(sess.roles) > 0 {
		out["roles"] = sess.roles
	}
	c.JSON(http.StatusOK, out)
}

// advanceMantleSession moves the session forward: resolve account, resolve
// role, then persist. It stops and records choices when input is needed.
func (h *Handler) advanceMantleSession(ctx context.Context, state string) {
	sess := mantleSessions.get(state)
	if sess == nil {
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	s := sess.storage
	tok := s.AccessToken
	if tok == "" || !IsOAuthSessionPending(state, mantleProvider) {
		return
	}

	if s.AccountID == "" {
		accounts, err := sess.client.ListAccounts(ctx, tok)
		if err != nil {
			SetOAuthSessionError(state, oauthSessionErrorWithCause("Failed to list accounts", err))
			mantleSessions.del(state)
			return
		}
		if len(accounts) == 0 {
			SetOAuthSessionError(state, "No AWS accounts are available")
			mantleSessions.del(state)
			return
		}
		if len(accounts) == 1 {
			s.AccountID, s.AccountName = accounts[0].ID, accounts[0].Name
		} else {
			sess.accounts = accounts
			sess.status = "select_account"
			return
		}
	} else if s.AccountName == "" {
		if accounts, err := sess.client.ListAccounts(ctx, tok); err == nil {
			for _, a := range accounts {
				if a.ID == s.AccountID {
					s.AccountName = a.Name
				}
			}
		}
	}
	if s.RoleName == "" {
		roles, err := sess.client.ListAccountRoles(ctx, tok, s.AccountID)
		if err != nil {
			SetOAuthSessionError(state, oauthSessionErrorWithCause("Failed to list roles", err))
			mantleSessions.del(state)
			return
		}
		if len(roles) == 0 {
			SetOAuthSessionError(state, "No AWS roles are available")
			mantleSessions.del(state)
			return
		}
		if len(roles) == 1 {
			s.RoleName = roles[0]
		} else {
			sess.roles = roles
			sess.status = "select_role"
			return
		}
	}

	s.Type = bedrockmantle.ProviderType
	s.Label = s.DefaultLabel()
	fileName := s.FileName()
	record := &coreauth.Auth{
		ID:       fileName,
		Provider: mantleProvider,
		FileName: fileName,
		Label:    s.Label,
		Prefix:   s.Prefix,
		Storage:  s,
		Metadata: s.ToMetadata(),
	}
	if sess.original != nil {
		current, ok := h.lookupAuthFile(sess.original.ID, "")
		if !ok || current == nil || current.Provider != mantleProvider {
			SetOAuthSessionError(state, "Credential was removed during login")
			mantleSessions.del(state)
			return
		}
		for _, key := range []string{"start_url", "sso_region", "account_id", "role_name"} {
			if current.Metadata[key] != sess.original.Metadata[key] {
				SetOAuthSessionError(state, "Credential changed during login; start again")
				mantleSessions.del(state)
				return
			}
		}
		record = mantleReauthenticatedRecord(current, s)
	}
	if errGuard := guardOAuthSessionPendingForSave(state, mantleProvider); errGuard != nil {
		mantleSessions.del(state)
		return
	}
	savedPath, errSave := h.saveTokenRecord(ctx, record)
	if errSave != nil {
		log.Errorf("Failed to save Bedrock Mantle credential: %v", errSave)
		SetOAuthSessionError(state, "Failed to save token to file")
		mantleSessions.del(state)
		return
	}
	sess.status = "done"
	CompleteOAuthSession(state)
	mantleSessions.del(state)
	fmt.Printf("Bedrock Mantle credential saved to %s\n", savedPath)
}

// AddBedrockMantleKey stores a static-key credential.
// Form body: access_key_id, secret_access_key, session_token, role_arn, prefix, region.
func (h *Handler) AddBedrockMantleKey(c *gin.Context) {
	pick := func(k string) string { return strings.TrimSpace(c.PostForm(k)) }
	s := &bedrockmantle.Storage{
		Type:            bedrockmantle.ProviderType,
		AuthMode:        bedrockmantle.ModeStatic,
		AccessKeyID:     pick("access_key_id"),
		SecretAccessKey: pick("secret_access_key"),
		SessionToken:    pick("session_token"),
		RoleARN:         pick("role_arn"),
		Prefix:          strings.Trim(pick("prefix"), "/"),
		DefaultRegion:   pick("region"),
	}
	if s.AccessKeyID == "" || s.SecretAccessKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "access_key_id and secret_access_key are required"})
		return
	}
	if err := applyMantleSetupRegions(s, c.PostForm("model_regions")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.Label = s.DefaultLabel()
	fileName := s.FileName()
	record := &coreauth.Auth{
		ID:       fileName,
		Provider: mantleProvider,
		FileName: fileName,
		Label:    s.Label,
		Prefix:   s.Prefix,
		Storage:  s,
		Metadata: s.ToMetadata(),
	}
	savedPath, err := h.saveTokenRecord(PopulateAuthContext(context.Background(), c), record)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save credential"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "auth-file": savedPath, "label": s.Label})
}
