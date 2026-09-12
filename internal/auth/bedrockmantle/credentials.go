package bedrockmantle

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// ErrSSOTokenExpired signals that the IdC access token is no longer usable
// and a refresh or re-login is required.
var ErrSSOTokenExpired = errors.New("bedrock mantle sso: access token expired")

// ProviderCache builds and caches aws.CredentialsProvider per credential.
type ProviderCache struct {
	mu         sync.Mutex
	providers  map[string]aws.CredentialsProvider
	httpClient *http.Client
}

// NewProviderCache constructs an empty cache.
func NewProviderCache(httpClient *http.Client) *ProviderCache {
	return &ProviderCache{
		providers:  make(map[string]aws.CredentialsProvider),
		httpClient: httpClient,
	}
}

// Invalidate drops a cached provider so the next call rebuilds it.
func (c *ProviderCache) Invalidate(key string) {
	c.mu.Lock()
	delete(c.providers, key)
	c.mu.Unlock()
}

// Retrieve returns AWS credentials for the storage, building and caching the
// underlying provider on first use.
func (c *ProviderCache) Retrieve(ctx context.Context, key string, s *Storage) (aws.Credentials, error) {
	if s == nil {
		return aws.Credentials{}, errors.New("bedrock mantle: credential storage is nil")
	}
	c.mu.Lock()
	p := c.providers[key]
	if p == nil {
		var err error
		p, err = c.build(ctx, s)
		if err != nil {
			c.mu.Unlock()
			return aws.Credentials{}, err
		}
		c.providers[key] = p
	}
	c.mu.Unlock()
	return p.Retrieve(ctx)
}

func (c *ProviderCache) build(ctx context.Context, s *Storage) (aws.CredentialsProvider, error) {
	var base aws.CredentialsProvider
	region := s.ResolveRegion("")

	switch DetectMode(s) {
	case ModeStatic:
		base = credentials.NewStaticCredentialsProvider(s.AccessKeyID, s.SecretAccessKey, s.SessionToken)
	case ModeProfile:
		opts := []func(*awsconfig.LoadOptions) error{
			awsconfig.WithSharedConfigProfile(strings.TrimSpace(s.Profile)),
		}
		if dir := expandHome(s.AWSDir); dir != "" {
			opts = append(opts,
				awsconfig.WithSharedConfigFiles([]string{filepath.Join(dir, "config")}),
				awsconfig.WithSharedCredentialsFiles([]string{filepath.Join(dir, "credentials")}),
			)
		}
		if c.httpClient != nil {
			opts = append(opts, awsconfig.WithHTTPClient(c.httpClient))
		}
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("bedrock mantle: load aws profile %q: %w", s.Profile, err)
		}
		// Profiles handle their own role chain; return as-is.
		return awsCfg.Credentials, nil
	case ModeSSO:
		base = aws.NewCredentialsCache(&ssoRoleProvider{storage: s, httpClient: c.httpClient})
	default:
		return nil, errors.New("bedrock mantle: credential has no usable auth mode")
	}

	if arn := strings.TrimSpace(s.RoleARN); arn != "" {
		stsCfg := aws.Config{Region: region, Credentials: base}
		if c.httpClient != nil {
			stsCfg.HTTPClient = c.httpClient
		}
		return aws.NewCredentialsCache(stscreds.NewAssumeRoleProvider(sts.NewFromConfig(stsCfg), arn, func(o *stscreds.AssumeRoleOptions) {
			o.RoleSessionName = "cli-proxy-api"
		})), nil
	}
	return base, nil
}

// ssoRoleProvider fetches role credentials from the IdC portal using the
// access token stored in the credential file. It does not refresh the token
// itself; the auth manager's Refresh loop owns that.
type ssoRoleProvider struct {
	storage    *Storage
	httpClient *http.Client
}

func (p *ssoRoleProvider) Retrieve(ctx context.Context) (aws.Credentials, error) {
	s := p.storage
	if strings.TrimSpace(s.AccessToken) == "" {
		return aws.Credentials{}, ErrSSOTokenExpired
	}
	if exp, err := time.Parse(time.RFC3339, s.Expired); err == nil && time.Now().After(exp) {
		return aws.Credentials{}, ErrSSOTokenExpired
	}
	client := NewClient(s.SSORegion, p.httpClient)
	return client.GetRoleCredentials(ctx, s.AccessToken, s.AccountID, s.RoleName)
}

func expandHome(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
