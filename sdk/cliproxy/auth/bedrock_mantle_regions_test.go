package auth

import (
	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/bedrockmantle"
	"testing"
)

func TestMantleRegionLookupAfterCredentialPrefix(t *testing.T) {
	a := &Auth{Provider: "bedrock-mantle", Prefix: "work"}
	storage := &bedrockmantle.Storage{DefaultRegion: "us-east-1", ModelRegions: map[string]string{"openai.gpt-5.6-luna": "us-west-2"}}
	model := rewriteModelForAuth("work/openai.gpt-5.6-luna", a)
	if model != "openai.gpt-5.6-luna" || storage.ResolveRegion(model) != "us-west-2" {
		t.Fatal("credential prefix interferes with region lookup")
	}
	if storage.ResolveRegion(rewriteModelForAuth("work/openai.gpt-5.4", a)) != "us-east-1" {
		t.Fatal("default region not used")
	}
	delete(storage.ModelRegions, model)
	if storage.ResolveRegion(model) != "us-east-1" {
		t.Fatal("removed override still applied")
	}
	storage.DefaultRegion = ""
	if storage.ResolveRegion(model) != "us-east-1" {
		t.Fatal("fallback region not used")
	}
}
