package management

import (
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/bedrockmantle"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func mantlePublicMetadata(auth *coreauth.Auth) gin.H {
	if auth == nil || auth.Provider != mantleProvider {
		return nil
	}
	out := gin.H{"prefix": auth.Prefix}
	for _, key := range []string{"auth_mode", "account_id", "account_name", "role_name", "role_arn", "default_region", "expired"} {
		if value, ok := auth.Metadata[key].(string); ok {
			out[key] = value
		}
	}
	if storage := bedrockmantle.FromMetadata(auth.Metadata); storage != nil && storage.ModelRegions != nil {
		out["model_regions"] = storage.ModelRegions
	}
	return out
}

func mantleReauthenticatedRecord(original *coreauth.Auth, storage *bedrockmantle.Storage) *coreauth.Auth {
	record := original.Clone()
	record.Storage = nil
	metadata := storage.ToMetadata()
	for _, key := range []string{"client_id", "client_secret", "client_secret_expires_at", "access_token", "refresh_token", "expired", "last_refresh"} {
		record.Metadata[key] = metadata[key]
	}
	return record
}

func applyMantleSelection(sess *mantlePendingSession, accountID, roleName string) bool {
	if sess.storage.AccessToken == "" {
		return false
	}
	switch sess.status {
	case "select_account":
		if roleName != "" {
			return false
		}
		for _, account := range sess.accounts {
			if account.ID == accountID {
				sess.storage.AccountID = account.ID
				sess.storage.AccountName = account.Name
				sess.storage.RoleName = ""
				sess.accounts = nil
				sess.status = "pending"
				return true
			}
		}
	case "select_role":
		if accountID != "" {
			return false
		}
		for _, role := range sess.roles {
			if role == roleName {
				sess.storage.RoleName = role
				sess.roles = nil
				sess.status = "pending"
				return true
			}
		}
	}
	return false
}
