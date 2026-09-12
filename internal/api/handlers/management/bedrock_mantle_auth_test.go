package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/bedrockmantle"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	sdkauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestMantleMetadataExcludesSecrets(t *testing.T) {
	auth := &coreauth.Auth{Provider: mantleProvider, Prefix: "work", Metadata: map[string]any{
		"auth_mode": "sso", "account_name": "Example", "expired": "2026-10-01T00:00:00Z",
		"access_token": "SECRET", "refresh_token": "SECRET", "client_secret": "SECRET",
		"access_key_id": "SECRET", "secret_access_key": "SECRET", "session_token": "SECRET",
	}}
	got := mantlePublicMetadata(auth)
	want := gin.H{"prefix": "work", "auth_mode": "sso", "account_name": "Example", "expired": "2026-10-01T00:00:00Z"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected public metadata keys")
	}
	if mantlePublicMetadata(&coreauth.Auth{Provider: "codex"}) != nil {
		t.Fatal("wrong provider included")
	}
}

func TestMantleReauthPreservesIdentityAndSettings(t *testing.T) {
	original := &coreauth.Auth{ID: "original.json", FileName: "original.json", Provider: mantleProvider,
		Prefix: "work", Label: "Custom label", Disabled: true, Status: coreauth.StatusDisabled,
		Metadata: map[string]any{
			"type": mantleProvider, "auth_mode": "sso", "access_token": "old",
			"prefix": "work", "disabled": true, "default_region": "eu-west-1",
			"model_regions": map[string]any{"openai.gpt-oss-20b": "us-east-1"},
			"weight":        float64(2), "note": "keep", "role_arn": "arn:aws:iam::123456789012:role/chained",
		},
	}
	storage := &bedrockmantle.Storage{AuthMode: "sso", AccessToken: "new", RefreshToken: "new-refresh", ClientID: "new-client", ClientSecret: "new-secret", Expired: "2026-10-01T00:00:00Z"}
	record := mantleReauthenticatedRecord(original, storage)
	if record.ID != original.ID || record.FileName != original.FileName || record.Label != original.Label || !record.Disabled {
		t.Fatal("reauth changed identity or disabled state")
	}
	for _, key := range []string{"prefix", "disabled", "default_region", "model_regions", "weight", "note", "role_arn"} {
		if !reflect.DeepEqual(record.Metadata[key], original.Metadata[key]) {
			t.Errorf("setting %s changed", key)
		}
	}
	if original.Metadata["access_token"] != "old" || record.Metadata["access_token"] != "new" {
		t.Fatal("token replacement mutated original")
	}
	store := sdkauth.NewFileTokenStore()
	store.SetBaseDir(t.TempDir())
	path, err := store.Save(coreauth.WithAuthCreationIntent(context.Background()), record)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "original.json" || saved["access_token"] != "new" || saved["disabled"] != true || saved["note"] != "keep" {
		t.Fatal("reauth did not persist token with settings")
	}
}

func TestMantleReauthSavesExistingFileWithoutFileName(t *testing.T) {
	dir := t.TempDir()
	store := sdkauth.NewFileTokenStore()
	store.SetBaseDir(dir)
	manager := coreauth.NewManager(nil, nil, nil)
	original := &coreauth.Auth{ID: "existing.json", Provider: mantleProvider, Prefix: "work", Metadata: map[string]any{
		"type": mantleProvider, "auth_mode": "sso", "start_url": "https://example.awsapps.com/start",
		"sso_region": "us-east-1", "account_id": "123456789012", "account_name": "Example", "role_name": "Bedrock",
		"access_token": "old", "client_secret": "old-client-secret", "note": "keep",
	}}
	if _, err := manager.Register(context.Background(), original); err != nil {
		t.Fatal(err)
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: dir}, manager)
	h.tokenStore = store
	state := "mantle-test-reauth-save"
	RegisterOAuthSession(state, mantleProvider)
	storage := bedrockmantle.FromMetadata(original.Metadata)
	storage.AccessToken = "new"
	storage.ClientSecret = "new-client-secret"
	mantleSessions.put(state, &mantlePendingSession{storage: storage, original: original})
	t.Cleanup(func() { mantleSessions.del(state); CancelOAuthSession(state) })
	h.advanceMantleSession(context.Background(), state)
	_, status, _, _, complete, ok := GetOAuthSessionDetails(state)
	if !ok || !complete || status != "" {
		t.Fatalf("login did not complete: %s", status)
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name() != "existing.json" {
		t.Fatal("reauth created wrong file")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "existing.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["access_token"] != "new" || saved["client_secret"] != "new-client-secret" || saved["note"] != "keep" {
		t.Fatal("reauth token or settings lost")
	}
}

func TestMantleSelectionsRequireApprovedListedChoices(t *testing.T) {
	sess := &mantlePendingSession{storage: &bedrockmantle.Storage{}, status: "pending"}
	if applyMantleSelection(sess, "123", "Admin") {
		t.Fatal("accepted before approval")
	}
	sess.storage.AccessToken = "token"
	sess.status = "select_account"
	sess.accounts = []bedrockmantle.Account{{ID: "123", Name: "Example"}}
	if applyMantleSelection(sess, "456", "") {
		t.Fatal("accepted unknown account")
	}
	if applyMantleSelection(sess, "123", "Admin") {
		t.Fatal("accepted injected role")
	}
	if !applyMantleSelection(sess, "123", "") || sess.storage.AccountName != "Example" {
		t.Fatal("rejected listed account")
	}
	if applyMantleSelection(sess, "123", "") {
		t.Fatal("accepted duplicate selection")
	}
	sess.status = "select_role"
	sess.roles = []string{"Bedrock"}
	if applyMantleSelection(sess, "", "Admin") {
		t.Fatal("accepted unknown role")
	}
	if !applyMantleSelection(sess, "", "Bedrock") {
		t.Fatal("rejected listed role")
	}
}

func TestMantleChoicesContractAndExpiry(t *testing.T) {
	state := "mantle-test-choices"
	RegisterOAuthSession(state, mantleProvider)
	sess := &mantlePendingSession{storage: &bedrockmantle.Storage{AccessToken: "SECRET"}, accounts: []bedrockmantle.Account{{ID: "123", Name: "Example", Email: "private@example.test"}}}
	mantleSessions.put(state, sess)
	t.Cleanup(func() { mantleSessions.del(state); CancelOAuthSession(state) })
	sess.status = "select_account"
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/bedrock-mantle-choices?state="+state, nil)
	(&Handler{}).GetBedrockMantleChoices(ctx)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"id":"123"`) || strings.Contains(rec.Body.String(), "SECRET") || strings.Contains(rec.Body.String(), "private@") {
		t.Fatal("invalid choices response")
	}
	sess.created = time.Now().Add(-mantleSessionTTL - time.Second)
	if mantleSessions.get(state) != nil {
		t.Fatal("expired session retained")
	}
	mantleSessions.put(state, sess)
	CancelOAuthSession(state)
	if mantleSessions.get(state) != nil {
		t.Fatal("cancelled session retained")
	}
}

func TestMantleStaticKeyBodyOnly(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/bedrock-mantle-key?access_key_id=example&secret_access_key=secret", nil)
	(&Handler{}).AddBedrockMantleKey(ctx)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("query keys accepted: %d", rec.Code)
	}
}

func TestMantleSelectBeforeApprovalRejected(t *testing.T) {
	state := "mantle-test-pending"
	RegisterOAuthSession(state, mantleProvider)
	mantleSessions.put(state, &mantlePendingSession{storage: &bedrockmantle.Storage{}})
	t.Cleanup(func() { mantleSessions.del(state); CancelOAuthSession(state) })
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	body := url.Values{"state": {state}, "account_id": {"123"}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/bedrock-mantle-select", strings.NewReader(body.Encode()))
	ctx.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	(&Handler{}).SelectBedrockMantleTarget(ctx)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("pending selection accepted: %d", rec.Code)
	}
}

func TestMantleReauthRejectsOtherProviders(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	_, err := manager.Register(context.Background(), &coreauth.Auth{ID: "codex.json", FileName: "codex.json", Provider: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/bedrock-mantle-auth-url?auth_file=codex.json", nil)
	h.RequestBedrockMantleToken(ctx)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("wrong provider accepted: %d", rec.Code)
	}
}
