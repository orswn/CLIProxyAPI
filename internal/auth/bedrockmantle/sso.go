// Package bedrockmantle implements AWS IAM Identity Center (SSO) device
// authorization and credential resolution for Bedrock Mantle without
// requiring the AWS CLI on the host.
package bedrockmantle

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sso"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	oidctypes "github.com/aws/aws-sdk-go-v2/service/ssooidc/types"
)

const (
	ProviderType = "bedrock-mantle"

	DefaultSSORegion     = "us-east-1"
	DefaultBedrockRegion = "us-east-1"

	clientName         = "cli-proxy-api"
	clientType         = "public"
	grantDeviceCode    = "urn:ietf:params:oauth:grant-type:device_code"
	grantRefreshToken  = "refresh_token"
	scopeAccountAccess = "sso:account:access"

	defaultPollInterval = 5 * time.Second
	maxDeviceWait       = 15 * time.Minute
	// RefreshLead triggers an SSO access token refresh this long before expiry.
	RefreshLead = 10 * time.Minute
)

// DeviceAuthorization holds an in-flight device flow.
type DeviceAuthorization struct {
	ClientID                string
	ClientSecret            string
	ClientSecretExpiresAt   time.Time
	DeviceCode              string
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	Interval                time.Duration
	ExpiresAt               time.Time
}

// Token is an SSO OIDC access token pair.
type Token struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// Account is an IdC account visible to the token.
type Account struct {
	ID    string
	Name  string
	Email string
}

// Session is the persisted SSO session for one credential file.
type Session struct {
	StartURL              string
	SSORegion             string
	ClientID              string
	ClientSecret          string
	ClientSecretExpiresAt time.Time
	Token                 Token
}

// Client wraps SSO OIDC and SSO portal clients for one region.
type Client struct {
	oidc *ssooidc.Client
	sso  *sso.Client
}

// NewClient builds anonymous SSO clients for the given IdC region.
func NewClient(region string, httpClient *http.Client) *Client {
	region = strings.TrimSpace(region)
	if region == "" {
		region = DefaultSSORegion
	}
	cfg := aws.Config{
		Region:      region,
		Credentials: aws.AnonymousCredentials{},
	}
	if httpClient != nil {
		cfg.HTTPClient = httpClient
	}
	return &Client{
		oidc: ssooidc.NewFromConfig(cfg),
		sso:  sso.NewFromConfig(cfg),
	}
}

// StartDeviceAuthorization registers a public client and starts the device flow.
func (c *Client) StartDeviceAuthorization(ctx context.Context, startURL string) (*DeviceAuthorization, error) {
	startURL = strings.TrimSpace(startURL)
	if startURL == "" {
		return nil, errors.New("bedrock mantle sso: start url is required")
	}
	reg, err := c.oidc.RegisterClient(ctx, &ssooidc.RegisterClientInput{
		ClientName: aws.String(clientName),
		ClientType: aws.String(clientType),
		Scopes:     []string{scopeAccountAccess},
		GrantTypes: []string{grantDeviceCode, grantRefreshToken},
	})
	if err != nil {
		return nil, fmt.Errorf("bedrock mantle sso: register client: %w", err)
	}
	start, err := c.oidc.StartDeviceAuthorization(ctx, &ssooidc.StartDeviceAuthorizationInput{
		ClientId:     reg.ClientId,
		ClientSecret: reg.ClientSecret,
		StartUrl:     aws.String(startURL),
	})
	if err != nil {
		return nil, fmt.Errorf("bedrock mantle sso: start device authorization: %w", err)
	}
	interval := defaultPollInterval
	if start.Interval > 0 {
		interval = time.Duration(start.Interval) * time.Second
	}
	expires := time.Now().Add(maxDeviceWait)
	if start.ExpiresIn > 0 {
		expires = time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)
	}
	return &DeviceAuthorization{
		ClientID:                aws.ToString(reg.ClientId),
		ClientSecret:            aws.ToString(reg.ClientSecret),
		ClientSecretExpiresAt:   time.Unix(reg.ClientSecretExpiresAt, 0).UTC(),
		DeviceCode:              aws.ToString(start.DeviceCode),
		UserCode:                aws.ToString(start.UserCode),
		VerificationURI:         aws.ToString(start.VerificationUri),
		VerificationURIComplete: aws.ToString(start.VerificationUriComplete),
		Interval:                interval,
		ExpiresAt:               expires,
	}, nil
}

// WaitForToken polls CreateToken until the user approves, the flow expires,
// or ctx is cancelled.
func (c *Client) WaitForToken(ctx context.Context, da *DeviceAuthorization) (*Token, error) {
	if da == nil {
		return nil, errors.New("bedrock mantle sso: device authorization is nil")
	}
	interval := da.Interval
	if interval <= 0 {
		interval = defaultPollInterval
	}
	for {
		if !da.ExpiresAt.IsZero() && time.Now().After(da.ExpiresAt) {
			return nil, errors.New("bedrock mantle sso: device authorization expired")
		}
		out, err := c.oidc.CreateToken(ctx, &ssooidc.CreateTokenInput{
			ClientId:     aws.String(da.ClientID),
			ClientSecret: aws.String(da.ClientSecret),
			GrantType:    aws.String(grantDeviceCode),
			DeviceCode:   aws.String(da.DeviceCode),
		})
		if err == nil {
			return tokenFromOutput(out), nil
		}
		var pending *oidctypes.AuthorizationPendingException
		var slow *oidctypes.SlowDownException
		switch {
		case errors.As(err, &pending):
		case errors.As(err, &slow):
			interval += defaultPollInterval
		default:
			return nil, fmt.Errorf("bedrock mantle sso: create token: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// RefreshToken exchanges a refresh token for a new access token.
func (c *Client) RefreshToken(ctx context.Context, s *Session) (*Token, error) {
	if s == nil {
		return nil, errors.New("bedrock mantle sso: session is nil")
	}
	if strings.TrimSpace(s.Token.RefreshToken) == "" {
		return nil, errors.New("bedrock mantle sso: refresh token missing")
	}
	if !s.ClientSecretExpiresAt.IsZero() && time.Now().After(s.ClientSecretExpiresAt) {
		return nil, errors.New("bedrock mantle sso: client registration expired; re-login required")
	}
	out, err := c.oidc.CreateToken(ctx, &ssooidc.CreateTokenInput{
		ClientId:     aws.String(s.ClientID),
		ClientSecret: aws.String(s.ClientSecret),
		GrantType:    aws.String(grantRefreshToken),
		RefreshToken: aws.String(s.Token.RefreshToken),
	})
	if err != nil {
		return nil, fmt.Errorf("bedrock mantle sso: refresh token: %w", err)
	}
	tok := tokenFromOutput(out)
	if tok.RefreshToken == "" {
		tok.RefreshToken = s.Token.RefreshToken
	}
	return tok, nil
}

// ListAccounts returns all accounts visible to the access token.
func (c *Client) ListAccounts(ctx context.Context, accessToken string) ([]Account, error) {
	var out []Account
	p := sso.NewListAccountsPaginator(c.sso, &sso.ListAccountsInput{AccessToken: aws.String(accessToken)})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("bedrock mantle sso: list accounts: %w", err)
		}
		for _, a := range page.AccountList {
			out = append(out, Account{
				ID:    aws.ToString(a.AccountId),
				Name:  aws.ToString(a.AccountName),
				Email: aws.ToString(a.EmailAddress),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ListAccountRoles returns role names for one account.
func (c *Client) ListAccountRoles(ctx context.Context, accessToken, accountID string) ([]string, error) {
	var out []string
	p := sso.NewListAccountRolesPaginator(c.sso, &sso.ListAccountRolesInput{
		AccessToken: aws.String(accessToken),
		AccountId:   aws.String(accountID),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("bedrock mantle sso: list account roles: %w", err)
		}
		for _, r := range page.RoleList {
			out = append(out, aws.ToString(r.RoleName))
		}
	}
	sort.Strings(out)
	return out, nil
}

// GetRoleCredentials returns temporary AWS credentials for the IdC role.
func (c *Client) GetRoleCredentials(ctx context.Context, accessToken, accountID, roleName string) (aws.Credentials, error) {
	out, err := c.sso.GetRoleCredentials(ctx, &sso.GetRoleCredentialsInput{
		AccessToken: aws.String(accessToken),
		AccountId:   aws.String(accountID),
		RoleName:    aws.String(roleName),
	})
	if err != nil {
		return aws.Credentials{}, fmt.Errorf("bedrock mantle sso: get role credentials: %w", err)
	}
	rc := out.RoleCredentials
	if rc == nil {
		return aws.Credentials{}, errors.New("bedrock mantle sso: empty role credentials")
	}
	return aws.Credentials{
		AccessKeyID:     aws.ToString(rc.AccessKeyId),
		SecretAccessKey: aws.ToString(rc.SecretAccessKey),
		SessionToken:    aws.ToString(rc.SessionToken),
		CanExpire:       rc.Expiration > 0,
		Expires:         time.UnixMilli(rc.Expiration).UTC(),
		Source:          "BedrockMantleSSO",
	}, nil
}

func tokenFromOutput(out *ssooidc.CreateTokenOutput) *Token {
	tok := &Token{
		AccessToken:  aws.ToString(out.AccessToken),
		RefreshToken: aws.ToString(out.RefreshToken),
	}
	if out.ExpiresIn > 0 {
		tok.ExpiresAt = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second).UTC()
	}
	return tok
}
