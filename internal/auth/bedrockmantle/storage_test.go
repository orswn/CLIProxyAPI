package bedrockmantle

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStorageRoundTripSSO(t *testing.T) {
	s := &Storage{
		AuthMode:      ModeSSO,
		Prefix:        "work",
		DefaultRegion: "us-east-1",
		ModelRegions:  map[string]string{"gpt-6-astra": "us-west-2"},
		StartURL:      "https://example.awsapps.com/start",
		SSORegion:     "us-east-1",
		AccountID:     "123456789012",
		AccountName:   "Example Inc",
		RoleName:      "BedrockRole",
		RoleARN:       "arn:aws:iam::123456789012:role/chained",
	}
	s.ApplySession(&Session{
		ClientID:              "cid",
		ClientSecret:          "csecret",
		ClientSecretExpiresAt: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		Token: Token{
			AccessToken:  "at",
			RefreshToken: "rt",
			ExpiresAt:    time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC),
		},
	})

	dir := t.TempDir()
	path := filepath.Join(dir, s.FileName())
	if err := s.SaveTokenToFile(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if filepath.Base(path) != "bedrock-mantle-work-123456789012-bedrockrole.json" {
		t.Fatalf("unexpected file name %s", filepath.Base(path))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["type"] != ProviderType {
		t.Fatalf("type = %v", meta["type"])
	}
	if meta["expired"] != "2026-09-13T00:00:00Z" {
		t.Fatalf("expired = %v", meta["expired"])
	}
	if _, ok := meta["access_key_id"]; ok {
		t.Fatalf("static fields must be omitted for sso mode")
	}

	back := FromMetadata(meta)
	if back == nil || back.AuthMode != ModeSSO {
		t.Fatalf("FromMetadata mode = %v", back)
	}
	if back.ResolveRegion("gpt-6-astra") != "us-west-2" || back.ResolveRegion("other") != "us-east-1" {
		t.Fatalf("region resolution mismatch")
	}
	sess := back.SSOSession()
	if sess.Token.RefreshToken != "rt" || sess.ClientSecret != "csecret" || sess.Token.ExpiresAt.IsZero() {
		t.Fatalf("session mismatch: %+v", sess)
	}
	if back.DefaultLabel() != "Example Inc / BedrockRole" {
		t.Fatalf("label = %q", back.DefaultLabel())
	}
}

func TestDetectMode(t *testing.T) {
	cases := []struct {
		s    Storage
		want string
	}{
		{Storage{AccessKeyID: "AKIA", SecretAccessKey: "x"}, ModeStatic},
		{Storage{Profile: "p"}, ModeProfile},
		{Storage{StartURL: "https://x"}, ModeSSO},
		{Storage{AccessToken: "tok"}, ModeSSO},
		{Storage{AuthMode: "SSO"}, ModeSSO},
		{Storage{}, ""},
	}
	for i, c := range cases {
		if got := DetectMode(&c.s); got != c.want {
			t.Errorf("case %d: got %q want %q", i, got, c.want)
		}
	}
}

func TestInspectProfileSSOChain(t *testing.T) {
	dir := t.TempDir()
	cfg := `[sso-session corp]
sso_start_url = https://corp.awsapps.com/start
sso_region = eu-west-1
sso_registration_scopes = sso:account:access

[profile base]
sso_session = corp
sso_account_id = 111122223333
sso_role_name = BaseRole
region = us-east-1

[profile chained]
role_arn = arn:aws:iam::111122223333:role/chained
source_profile = base
region = us-west-2

[profile static]
aws_access_key_id = AKIASTATIC
aws_secret_access_key = secret
region = ap-southeast-1
`
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "credentials"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	imp, err := InspectProfile(context.Background(), "chained", dir)
	if err != nil {
		t.Fatalf("inspect chained: %v", err)
	}
	if !imp.NeedsLogin || imp.StartURL != "https://corp.awsapps.com/start" || imp.SSORegion != "eu-west-1" {
		t.Fatalf("sso fields: %+v", imp)
	}
	if imp.AccountID != "111122223333" || imp.RoleName != "BaseRole" {
		t.Fatalf("account/role: %+v", imp)
	}
	if imp.RoleARN != "arn:aws:iam::111122223333:role/chained" || imp.Region != "us-west-2" {
		t.Fatalf("role arn/region: %+v", imp)
	}
	st := imp.ToStorage("work")
	if st.AuthMode != ModeSSO || st.Prefix != "work" || st.RoleARN == "" {
		t.Fatalf("storage: %+v", st)
	}

	imp, err = InspectProfile(context.Background(), "static", dir)
	if err != nil {
		t.Fatalf("inspect static: %v", err)
	}
	if imp.NeedsLogin || imp.AccessKeyID != "AKIASTATIC" || imp.Region != "ap-southeast-1" {
		t.Fatalf("static: %+v", imp)
	}
	if imp.ToStorage("").AuthMode != ModeStatic {
		t.Fatalf("static storage mode")
	}
}

func TestSSORoleProviderRejectsExpiredToken(t *testing.T) {
	p := &ssoRoleProvider{storage: &Storage{
		AuthMode:    ModeSSO,
		AccessToken: "tok",
		Expired:     time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
	}}
	if _, err := p.Retrieve(context.Background()); err != ErrSSOTokenExpired {
		t.Fatalf("expected ErrSSOTokenExpired, got %v", err)
	}
}
