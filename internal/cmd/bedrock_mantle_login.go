package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v7/sdk/auth"
	log "github.com/sirupsen/logrus"
)

// MantleLoginOptions carries the non-interactive inputs for Bedrock Mantle onboarding.
type MantleLoginOptions struct {
	Mode      string // sso | static | profile | import
	Prefix    string
	Region    string
	StartURL  string
	SSORegion string
	AccountID string
	RoleName  string
	RoleARN   string
	AccessKey string
	SecretKey string
	Session   string
	Profile   string
	AWSDir    string
}

// DoBedrockMantleLogin runs the Bedrock Mantle onboarding flow and persists a
// self-contained auth file under auth-dir.
func DoBedrockMantleLogin(cfg *config.Config, options *LoginOptions, mantle *MantleLoginOptions) {
	if options == nil {
		options = &LoginOptions{}
	}
	if mantle == nil {
		mantle = &MantleLoginOptions{}
	}
	promptFn := options.Prompt
	if promptFn == nil {
		promptFn = defaultProjectPrompt()
	}

	metadata := map[string]string{
		sdkAuth.BedrockMantleModeKey:       strings.TrimSpace(mantle.Mode),
		sdkAuth.BedrockMantlePrefixKey:     strings.TrimSpace(mantle.Prefix),
		sdkAuth.BedrockMantleRegionKey:     strings.TrimSpace(mantle.Region),
		sdkAuth.BedrockMantleStartURLKey:   strings.TrimSpace(mantle.StartURL),
		sdkAuth.BedrockMantleSSORegionKey:  strings.TrimSpace(mantle.SSORegion),
		sdkAuth.BedrockMantleAccountIDKey:  strings.TrimSpace(mantle.AccountID),
		sdkAuth.BedrockMantleRoleNameKey:   strings.TrimSpace(mantle.RoleName),
		sdkAuth.BedrockMantleRoleARNKey:    strings.TrimSpace(mantle.RoleARN),
		sdkAuth.BedrockMantleAccessKeyKey:  strings.TrimSpace(mantle.AccessKey),
		sdkAuth.BedrockMantleSecretKeyKey:  strings.TrimSpace(mantle.SecretKey),
		sdkAuth.BedrockMantleSessionTokKey: strings.TrimSpace(mantle.Session),
		sdkAuth.BedrockMantleProfileKey:    strings.TrimSpace(mantle.Profile),
		sdkAuth.BedrockMantleAWSDirKey:     strings.TrimSpace(mantle.AWSDir),
	}

	manager := newAuthManager()
	authOpts := &sdkAuth.LoginOptions{
		NoBrowser:    options.NoBrowser,
		CallbackPort: options.CallbackPort,
		Metadata:     metadata,
		Prompt:       promptFn,
	}

	record, savedPath, err := manager.Login(context.Background(), "bedrock-mantle", cfg, authOpts)
	if err != nil {
		log.Errorf("Bedrock Mantle authentication failed: %v", err)
		return
	}
	if savedPath != "" {
		fmt.Printf("Credential saved to %s\n", savedPath)
	}
	if record != nil && record.Label != "" {
		fmt.Printf("Credential: %s\n", record.Label)
	}
	fmt.Println("Bedrock Mantle setup successful!")
}
