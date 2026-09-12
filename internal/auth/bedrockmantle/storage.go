package bedrockmantle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/misc"
	log "github.com/sirupsen/logrus"
)

// Metadata keys shared by auth files, config synthesis, and the executor.
const (
	KeyType            = "type"
	KeyAuthMode        = "auth_mode"
	KeyLabel           = "label"
	KeyPrefix          = "prefix"
	KeyDefaultRegion   = "default_region"
	KeyModelRegions    = "model_regions"
	KeyAccessKeyID     = "access_key_id"
	KeySecretAccessKey = "secret_access_key"
	KeySessionToken    = "session_token"
	KeyProfile         = "profile"
	KeyAWSDir          = "aws_dir"
	KeyStartURL        = "start_url"
	KeySSORegion       = "sso_region"
	KeyAccountID       = "account_id"
	KeyAccountName     = "account_name"
	KeyRoleName        = "role_name"
	KeyRoleARN         = "role_arn"
	KeyClientID        = "client_id"
	KeyClientSecret    = "client_secret"
	KeyClientSecretExp = "client_secret_expires_at"
	KeyAccessToken     = "access_token"
	KeyRefreshToken    = "refresh_token"
	KeyExpired         = "expired"
	KeyLastRefresh     = "last_refresh"
)

// Authentication modes.
const (
	ModeStatic  = "static"
	ModeProfile = "profile"
	ModeSSO     = "sso"
)

// Storage is the on-disk representation of one Bedrock Mantle credential.
type Storage struct {
	Type     string `json:"type"`
	AuthMode string `json:"auth_mode"`
	Label    string `json:"label,omitempty"`
	Prefix   string `json:"prefix,omitempty"`

	DefaultRegion string            `json:"default_region,omitempty"`
	ModelRegions  map[string]string `json:"model_regions,omitempty"`

	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	SessionToken    string `json:"session_token,omitempty"`

	Profile string `json:"profile,omitempty"`
	AWSDir  string `json:"aws_dir,omitempty"`

	StartURL              string `json:"start_url,omitempty"`
	SSORegion             string `json:"sso_region,omitempty"`
	AccountID             string `json:"account_id,omitempty"`
	AccountName           string `json:"account_name,omitempty"`
	RoleName              string `json:"role_name,omitempty"`
	RoleARN               string `json:"role_arn,omitempty"`
	ClientID              string `json:"client_id,omitempty"`
	ClientSecret          string `json:"client_secret,omitempty"`
	ClientSecretExpiresAt string `json:"client_secret_expires_at,omitempty"`
	AccessToken           string `json:"access_token,omitempty"`
	RefreshToken          string `json:"refresh_token,omitempty"`
	Expired               string `json:"expired,omitempty"`
	LastRefresh           string `json:"last_refresh,omitempty"`

	metadata map[string]any
}

// SetMetadata lets the token store inject runtime metadata before saving.
func (s *Storage) SetMetadata(meta map[string]any) { s.metadata = meta }

// SaveTokenToFile persists the credential as JSON.
func (s *Storage) SaveTokenToFile(path string) error {
	misc.LogSavingCredentials(path)
	if s == nil {
		return fmt.Errorf("bedrock mantle credential: storage is nil")
	}
	s.Type = ProviderType
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("bedrock mantle credential: create directory failed: %w", err)
	}
	data, err := misc.MergeMetadata(s, s.metadata)
	if err != nil {
		return fmt.Errorf("bedrock mantle credential: merge metadata failed: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("bedrock mantle credential: create file failed: %w", err)
	}
	defer func() {
		if errClose := f.Close(); errClose != nil {
			log.Errorf("bedrock mantle credential: failed to close file: %v", errClose)
		}
	}()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err = enc.Encode(data); err != nil {
		return fmt.Errorf("bedrock mantle credential: encode failed: %w", err)
	}
	return nil
}

// ToMetadata renders the storage as the auth metadata map.
func (s *Storage) ToMetadata() map[string]any {
	if s == nil {
		return nil
	}
	s.Type = ProviderType
	data, err := misc.MergeMetadata(s, nil)
	if err != nil {
		return nil
	}
	return data
}

// FileName returns a stable auth file name for this credential.
func (s *Storage) FileName() string {
	parts := []string{ProviderType}
	if p := sanitizeFilePart(s.Prefix); p != "" {
		parts = append(parts, p)
	}
	switch s.AuthMode {
	case ModeSSO:
		if a := sanitizeFilePart(s.AccountID); a != "" {
			parts = append(parts, a)
		}
		if r := sanitizeFilePart(s.RoleName); r != "" {
			parts = append(parts, r)
		}
	case ModeProfile:
		if p := sanitizeFilePart(s.Profile); p != "" {
			parts = append(parts, p)
		}
	case ModeStatic:
		if ak := s.AccessKeyID; len(ak) >= 8 {
			parts = append(parts, strings.ToLower(ak[len(ak)-8:]))
		}
	}
	return strings.Join(parts, "-") + ".json"
}

// DefaultLabel builds a human-readable label.
func (s *Storage) DefaultLabel() string {
	switch s.AuthMode {
	case ModeSSO:
		name := strings.TrimSpace(s.AccountName)
		if name == "" {
			name = strings.TrimSpace(s.AccountID)
		}
		if role := strings.TrimSpace(s.RoleName); role != "" {
			return fmt.Sprintf("%s / %s", name, role)
		}
		return name
	case ModeProfile:
		return "profile " + strings.TrimSpace(s.Profile)
	case ModeStatic:
		if ak := s.AccessKeyID; len(ak) >= 4 {
			return "key …" + ak[len(ak)-4:]
		}
	}
	return ProviderType
}

// DetectMode infers the auth mode from populated fields.
func DetectMode(s *Storage) string {
	if s == nil {
		return ""
	}
	if m := strings.ToLower(strings.TrimSpace(s.AuthMode)); m != "" {
		return m
	}
	switch {
	case strings.TrimSpace(s.StartURL) != "" || strings.TrimSpace(s.AccessToken) != "":
		return ModeSSO
	case strings.TrimSpace(s.AccessKeyID) != "" && strings.TrimSpace(s.SecretAccessKey) != "":
		return ModeStatic
	case strings.TrimSpace(s.Profile) != "":
		return ModeProfile
	}
	return ""
}

// FromMetadata decodes an auth metadata map into Storage.
func FromMetadata(meta map[string]any) *Storage {
	if len(meta) == 0 {
		return nil
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return nil
	}
	var s Storage
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil
	}
	s.AuthMode = DetectMode(&s)
	return &s
}

// ApplySession writes SSO session fields into the storage.
func (s *Storage) ApplySession(sess *Session) {
	if s == nil || sess == nil {
		return
	}
	s.StartURL = sess.StartURL
	s.SSORegion = sess.SSORegion
	s.ClientID = sess.ClientID
	s.ClientSecret = sess.ClientSecret
	if !sess.ClientSecretExpiresAt.IsZero() {
		s.ClientSecretExpiresAt = sess.ClientSecretExpiresAt.UTC().Format(time.RFC3339)
	}
	s.ApplyToken(&sess.Token)
}

// ApplyToken writes a refreshed token into the storage.
func (s *Storage) ApplyToken(tok *Token) {
	if s == nil || tok == nil {
		return
	}
	s.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		s.RefreshToken = tok.RefreshToken
	}
	if !tok.ExpiresAt.IsZero() {
		s.Expired = tok.ExpiresAt.UTC().Format(time.RFC3339)
	}
	s.LastRefresh = time.Now().UTC().Format(time.RFC3339)
}

// SSOSession extracts the SSO session from storage.
func (s *Storage) SSOSession() *Session {
	if s == nil {
		return nil
	}
	sess := &Session{
		StartURL:     s.StartURL,
		SSORegion:    s.SSORegion,
		ClientID:     s.ClientID,
		ClientSecret: s.ClientSecret,
		Token: Token{
			AccessToken:  s.AccessToken,
			RefreshToken: s.RefreshToken,
		},
	}
	if t, err := time.Parse(time.RFC3339, s.ClientSecretExpiresAt); err == nil {
		sess.ClientSecretExpiresAt = t
	}
	if t, err := time.Parse(time.RFC3339, s.Expired); err == nil {
		sess.Token.ExpiresAt = t
	}
	return sess
}

// ResolveRegion returns the AWS region for a model.
func (s *Storage) ResolveRegion(model string) string {
	model = strings.TrimSpace(model)
	if s != nil {
		for k, v := range s.ModelRegions {
			if strings.EqualFold(strings.TrimSpace(k), model) && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
		if r := strings.TrimSpace(s.DefaultRegion); r != "" {
			return r
		}
	}
	return DefaultBedrockRegion
}

func sanitizeFilePart(v string) string {
	out := strings.TrimSpace(v)
	for _, r := range []string{"/", "\\", ":", " ", "@"} {
		out = strings.ReplaceAll(out, r, "-")
	}
	return strings.Trim(strings.ToLower(out), "-")
}
