package bedrockmantle

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
)

// ProfileImport is what an existing AWS profile resolves to.
type ProfileImport struct {
	Profile string
	Region  string

	// Static: set when the chain bottoms out at long-lived keys.
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string

	// SSO: set when the chain bottoms out at an IdC session.
	StartURL  string
	SSORegion string
	AccountID string
	RoleName  string

	// RoleARN is the outermost assumed role, if any.
	RoleARN string

	// NeedsLogin is true when the profile is SSO based: the caller must run
	// the device flow to obtain a token owned by this process.
	NeedsLogin bool
}

// InspectProfile walks the source_profile chain of an AWS profile and reports
// how it authenticates. awsDir overrides ~/.aws when non-empty.
func InspectProfile(ctx context.Context, profile, awsDir string) (*ProfileImport, error) {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return nil, fmt.Errorf("bedrock mantle: profile name is required")
	}
	var opts []func(*awsconfig.LoadSharedConfigOptions)
	if dir := expandHome(awsDir); dir != "" {
		opts = append(opts, func(o *awsconfig.LoadSharedConfigOptions) {
			o.ConfigFiles = []string{filepath.Join(dir, "config")}
			o.CredentialsFiles = []string{filepath.Join(dir, "credentials")}
		})
	}
	sc, err := awsconfig.LoadSharedConfigProfile(ctx, profile, opts...)
	if err != nil {
		return nil, fmt.Errorf("bedrock mantle: load profile %q: %w", profile, err)
	}

	out := &ProfileImport{Profile: profile, Region: strings.TrimSpace(sc.Region)}
	if out.Region == "" {
		out.Region = DefaultBedrockRegion
	}

	cur := &sc
	for depth := 0; cur != nil && depth < 8; depth++ {
		if out.RoleARN == "" && strings.TrimSpace(cur.RoleARN) != "" {
			out.RoleARN = strings.TrimSpace(cur.RoleARN)
		}
		if cur.Credentials.HasKeys() {
			out.AccessKeyID = cur.Credentials.AccessKeyID
			out.SecretAccessKey = cur.Credentials.SecretAccessKey
			out.SessionToken = cur.Credentials.SessionToken
			return out, nil
		}
		if cur.SSOAccountID != "" || cur.SSORoleName != "" || cur.SSOStartURL != "" || cur.SSOSession != nil {
			out.AccountID = strings.TrimSpace(cur.SSOAccountID)
			out.RoleName = strings.TrimSpace(cur.SSORoleName)
			out.StartURL = strings.TrimSpace(cur.SSOStartURL)
			out.SSORegion = strings.TrimSpace(cur.SSORegion)
			if cur.SSOSession != nil {
				if out.StartURL == "" {
					out.StartURL = strings.TrimSpace(cur.SSOSession.SSOStartURL)
				}
				if out.SSORegion == "" {
					out.SSORegion = strings.TrimSpace(cur.SSOSession.SSORegion)
				}
			}
			if out.SSORegion == "" {
				out.SSORegion = DefaultSSORegion
			}
			// The IdC role is the base identity; an outer role_arn is assumed on top.
			if out.RoleARN != "" && strings.HasSuffix(out.RoleARN, "/"+out.RoleName) {
				out.RoleARN = ""
			}
			out.NeedsLogin = true
			return out, nil
		}
		if cur.CredentialProcess != "" || cur.CredentialSource != "" || cur.WebIdentityTokenFile != "" {
			// Cannot be embedded; fall back to profile mode at runtime.
			return out, nil
		}
		cur = cur.Source
	}
	return out, nil
}

// ToStorage converts an inspected profile into credential storage. SSO
// imports return storage without a token; the caller runs the device flow.
func (p *ProfileImport) ToStorage(prefix string) *Storage {
	s := &Storage{
		Type:          ProviderType,
		Prefix:        strings.Trim(strings.TrimSpace(prefix), "/"),
		DefaultRegion: p.Region,
		RoleARN:       p.RoleARN,
	}
	switch {
	case p.AccessKeyID != "" && p.SecretAccessKey != "":
		s.AuthMode = ModeStatic
		s.AccessKeyID = p.AccessKeyID
		s.SecretAccessKey = p.SecretAccessKey
		s.SessionToken = p.SessionToken
	case p.NeedsLogin:
		s.AuthMode = ModeSSO
		s.StartURL = p.StartURL
		s.SSORegion = p.SSORegion
		s.AccountID = p.AccountID
		s.RoleName = p.RoleName
	default:
		s.AuthMode = ModeProfile
		s.Profile = p.Profile
	}
	s.Label = s.DefaultLabel()
	return s
}
