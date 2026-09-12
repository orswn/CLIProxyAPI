package auth

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/bedrockmantle"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/browser"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// LoginOptions.Metadata keys understood by BedrockMantleAuthenticator.
const (
	BedrockMantleModeKey       = "mantle_mode" // sso | static | profile | import
	BedrockMantlePrefixKey     = "prefix"
	BedrockMantleRegionKey     = "region"
	BedrockMantleStartURLKey   = "start_url"
	BedrockMantleSSORegionKey  = "sso_region"
	BedrockMantleAccountIDKey  = "account_id"
	BedrockMantleRoleNameKey   = "role_name"
	BedrockMantleRoleARNKey    = "role_arn"
	BedrockMantleAccessKeyKey  = "access_key_id"
	BedrockMantleSecretKeyKey  = "secret_access_key"
	BedrockMantleSessionTokKey = "session_token"
	BedrockMantleProfileKey    = "profile"
	BedrockMantleAWSDirKey     = "aws_dir"
	BedrockMantleEmbedKey      = "embed" // "false" keeps profile mode instead of embedding
)

// BedrockMantleAuthenticator handles static keys, profile import, and the
// IAM Identity Center device flow for Bedrock Mantle.
type BedrockMantleAuthenticator struct{}

// NewBedrockMantleAuthenticator constructs the authenticator.
func NewBedrockMantleAuthenticator() Authenticator { return &BedrockMantleAuthenticator{} }

// Provider returns the provider key.
func (BedrockMantleAuthenticator) Provider() string { return bedrockmantle.ProviderType }

// RefreshLead asks the manager to refresh SSO tokens before expiry.
func (BedrockMantleAuthenticator) RefreshLead() *time.Duration {
	lead := bedrockmantle.RefreshLead
	return &lead
}

// Login runs the selected mode and returns an auth record ready to persist.
func (a BedrockMantleAuthenticator) Login(ctx context.Context, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts == nil {
		opts = &LoginOptions{}
	}
	meta := opts.Metadata
	if meta == nil {
		meta = map[string]string{}
	}
	prompt := opts.Prompt
	if prompt == nil {
		prompt = func(string) (string, error) { return "", nil }
	}
	get := func(k string) string { return strings.TrimSpace(meta[k]) }

	prefix := strings.Trim(get(BedrockMantlePrefixKey), "/")
	region := get(BedrockMantleRegionKey)
	mode := strings.ToLower(get(BedrockMantleModeKey))

	var storage *bedrockmantle.Storage

	switch mode {
	case "static":
		storage = &bedrockmantle.Storage{
			AuthMode:        bedrockmantle.ModeStatic,
			AccessKeyID:     get(BedrockMantleAccessKeyKey),
			SecretAccessKey: get(BedrockMantleSecretKeyKey),
			SessionToken:    get(BedrockMantleSessionTokKey),
			RoleARN:         get(BedrockMantleRoleARNKey),
		}
		if storage.AccessKeyID == "" || storage.SecretAccessKey == "" {
			return nil, fmt.Errorf("bedrock mantle: access_key_id and secret_access_key are required")
		}

	case "profile", "import":
		profile := get(BedrockMantleProfileKey)
		if profile == "" {
			return nil, fmt.Errorf("bedrock mantle: profile is required")
		}
		if mode == "profile" || strings.EqualFold(get(BedrockMantleEmbedKey), "false") {
			storage = &bedrockmantle.Storage{
				AuthMode: bedrockmantle.ModeProfile,
				Profile:  profile,
				AWSDir:   get(BedrockMantleAWSDirKey),
			}
			break
		}
		imp, err := bedrockmantle.InspectProfile(ctx, profile, get(BedrockMantleAWSDirKey))
		if err != nil {
			return nil, err
		}
		storage = imp.ToStorage(prefix)
		if region == "" {
			region = imp.Region
		}
		if storage.AuthMode == bedrockmantle.ModeSSO {
			fmt.Printf("Profile %q uses IAM Identity Center; starting device login for %s\n", profile, storage.StartURL)
			if err := a.deviceLogin(ctx, storage, opts, prompt); err != nil {
				return nil, err
			}
		}

	case "", "sso":
		storage = &bedrockmantle.Storage{
			AuthMode:  bedrockmantle.ModeSSO,
			StartURL:  get(BedrockMantleStartURLKey),
			SSORegion: get(BedrockMantleSSORegionKey),
			AccountID: get(BedrockMantleAccountIDKey),
			RoleName:  get(BedrockMantleRoleNameKey),
			RoleARN:   get(BedrockMantleRoleARNKey),
		}
		if storage.StartURL == "" {
			v, err := prompt("IAM Identity Center start URL (https://<org>.awsapps.com/start): ")
			if err != nil {
				return nil, err
			}
			storage.StartURL = strings.TrimSpace(v)
		}
		if storage.StartURL == "" {
			return nil, fmt.Errorf("bedrock mantle: start_url is required")
		}
		if storage.SSORegion == "" {
			v, _ := prompt(fmt.Sprintf("Identity Center region [%s]: ", bedrockmantle.DefaultSSORegion))
			storage.SSORegion = strings.TrimSpace(v)
		}
		if storage.SSORegion == "" {
			storage.SSORegion = bedrockmantle.DefaultSSORegion
		}
		if err := a.deviceLogin(ctx, storage, opts, prompt); err != nil {
			return nil, err
		}

	default:
		return nil, fmt.Errorf("bedrock mantle: unknown mode %q", mode)
	}

	if prefix == "" && storage.Prefix == "" {
		v, _ := prompt("Model prefix (e.g. work, personal; empty for none): ")
		prefix = strings.Trim(strings.TrimSpace(v), "/")
	}
	if prefix != "" {
		storage.Prefix = prefix
	}
	if region == "" {
		region = storage.DefaultRegion
	}
	if region == "" {
		v, _ := prompt(fmt.Sprintf("Bedrock region [%s]: ", bedrockmantle.DefaultBedrockRegion))
		region = strings.TrimSpace(v)
	}
	if region == "" {
		region = bedrockmantle.DefaultBedrockRegion
	}
	storage.DefaultRegion = region
	storage.Label = storage.DefaultLabel()
	storage.Type = bedrockmantle.ProviderType

	fileName := storage.FileName()
	metadata := storage.ToMetadata()
	return &coreauth.Auth{
		ID:       fileName,
		Provider: bedrockmantle.ProviderType,
		FileName: fileName,
		Label:    storage.Label,
		Prefix:   storage.Prefix,
		Storage:  storage,
		Metadata: metadata,
	}, nil
}

// deviceLogin runs the IdC device flow and fills token, account, and role.
func (a BedrockMantleAuthenticator) deviceLogin(ctx context.Context, s *bedrockmantle.Storage, opts *LoginOptions, prompt func(string) (string, error)) error {
	client := bedrockmantle.NewClient(s.SSORegion, nil)
	da, err := client.StartDeviceAuthorization(ctx, s.StartURL)
	if err != nil {
		return err
	}
	url := da.VerificationURIComplete
	if url == "" {
		url = da.VerificationURI
	}
	fmt.Printf("\nTo authenticate, open:\n%s\n", url)
	if da.VerificationURIComplete == "" || opts.NoBrowser {
		fmt.Printf("Enter code: %s\n", da.UserCode)
	}
	if !opts.NoBrowser && browser.IsAvailable() {
		if errOpen := browser.OpenURL(url); errOpen != nil {
			log.Warnf("Failed to open browser automatically: %v", errOpen)
		}
	}
	fmt.Println("Waiting for authorization...")
	tok, err := client.WaitForToken(ctx, da)
	if err != nil {
		return err
	}
	s.ApplySession(&bedrockmantle.Session{
		StartURL:              s.StartURL,
		SSORegion:             s.SSORegion,
		ClientID:              da.ClientID,
		ClientSecret:          da.ClientSecret,
		ClientSecretExpiresAt: da.ClientSecretExpiresAt,
		Token:                 *tok,
	})

	if s.AccountID == "" {
		accounts, err := client.ListAccounts(ctx, tok.AccessToken)
		if err != nil {
			return err
		}
		switch len(accounts) {
		case 0:
			return fmt.Errorf("bedrock mantle sso: no accounts assigned to this user")
		case 1:
			s.AccountID, s.AccountName = accounts[0].ID, accounts[0].Name
		default:
			fmt.Println("Accounts:")
			for i, acc := range accounts {
				fmt.Printf("  [%d] %s (%s)\n", i+1, acc.Name, acc.ID)
			}
			idx, err := pickIndex(prompt, "Select account: ", len(accounts))
			if err != nil {
				return err
			}
			s.AccountID, s.AccountName = accounts[idx].ID, accounts[idx].Name
		}
	} else if s.AccountName == "" {
		if accounts, err := client.ListAccounts(ctx, tok.AccessToken); err == nil {
			for _, acc := range accounts {
				if acc.ID == s.AccountID {
					s.AccountName = acc.Name
				}
			}
		}
	}

	if s.RoleName == "" {
		roles, err := client.ListAccountRoles(ctx, tok.AccessToken, s.AccountID)
		if err != nil {
			return err
		}
		switch len(roles) {
		case 0:
			return fmt.Errorf("bedrock mantle sso: no roles in account %s", s.AccountID)
		case 1:
			s.RoleName = roles[0]
		default:
			fmt.Println("Roles:")
			for i, r := range roles {
				fmt.Printf("  [%d] %s\n", i+1, r)
			}
			idx, err := pickIndex(prompt, "Select role: ", len(roles))
			if err != nil {
				return err
			}
			s.RoleName = roles[idx]
		}
	}
	fmt.Printf("Authenticated: %s / %s\n", s.AccountName, s.RoleName)
	return nil
}

func pickIndex(prompt func(string) (string, error), label string, n int) (int, error) {
	for {
		v, err := prompt(label)
		if err != nil {
			return 0, err
		}
		i, errConv := strconv.Atoi(strings.TrimSpace(v))
		if errConv == nil && i >= 1 && i <= n {
			return i - 1, nil
		}
		fmt.Printf("Enter a number between 1 and %d\n", n)
	}
}
