package cliproxy

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

func TestBuildConfigModelsAppliesPricing(t *testing.T) {
	entry := &config.ClaudeKey{
		Models: []config.ClaudeModel{
			{
				Name:  "claude-upstream",
				Alias: "claude-alias",
				Pricing: &registry.ModelPricing{
					Input:         3,
					Output:        15,
					Cached:        0.3,
					CacheCreation: 3.75,
					Reasoning:     15,
					LongContext: &registry.LongContextPricing{
						Threshold: 200000,
						Input:     6,
						Output:    22.5,
					},
				},
			},
			{Name: "claude-no-pricing", Alias: "claude-plain"},
		},
	}

	models := buildClaudeConfigModels(entry)
	if len(models) != 2 {
		t.Fatalf("models = %d, want 2", len(models))
	}
	priced := models[0]
	if priced.Pricing == nil {
		t.Fatal("configured pricing missing")
	}
	if priced.Pricing.Input != 3 || priced.Pricing.Output != 15 {
		t.Errorf("pricing = %+v, want input 3 output 15", *priced.Pricing)
	}
	if priced.Pricing.LongContext == nil {
		t.Fatal("configured long-context pricing missing")
	}
	if priced.Pricing.LongContext.Threshold != 200000 || priced.Pricing.LongContext.Output != 22.5 {
		t.Errorf("long-context pricing = %+v", *priced.Pricing.LongContext)
	}
	if models[1].Pricing != nil {
		t.Errorf("unpriced model pricing = %+v, want nil", *models[1].Pricing)
	}
}

func TestBuildOpenAICompatibilityConfigModelsAppliesPricing(t *testing.T) {
	compat := &config.OpenAICompatibility{
		Name: "custom-provider",
		Models: []config.OpenAICompatibilityModel{
			{
				Name:  "custom-upstream",
				Alias: "custom-alias",
				Pricing: &registry.ModelPricing{
					Input:  0.5,
					Output: 2,
					LongContext: &registry.LongContextPricing{
						Threshold: 128000,
						Input:     1,
						Output:    4,
					},
				},
			},
		},
	}

	models := buildOpenAICompatibilityConfigModels(compat)
	if len(models) != 1 {
		t.Fatalf("models = %d, want 1", len(models))
	}
	pricing := models[0].Pricing
	if pricing == nil {
		t.Fatal("configured pricing missing")
	}
	if pricing.Input != 0.5 || pricing.Output != 2 {
		t.Errorf("pricing = %+v, want input 0.5 output 2", *pricing)
	}
	if pricing.LongContext == nil || pricing.LongContext.Threshold != 128000 {
		t.Errorf("long-context pricing = %+v", pricing.LongContext)
	}
}

func TestBuildConfigModelsNormalizesPricing(t *testing.T) {
	entry := &config.CodexKey{
		Models: []config.CodexModel{
			{
				Name:    "codex-upstream",
				Alias:   "codex-alias",
				Pricing: &registry.ModelPricing{Input: -1, Output: 10, LongContext: &registry.LongContextPricing{Input: 2}},
			},
		},
	}

	models := buildCodexConfigModels(entry)
	if len(models) == 0 {
		t.Fatal("no models built")
	}
	pricing := models[0].Pricing
	if pricing == nil {
		t.Fatal("configured pricing missing")
	}
	if pricing.Input != 0 {
		t.Errorf("input = %v, want 0 for a negative rate", pricing.Input)
	}
	if pricing.LongContext != nil {
		t.Errorf("long context = %+v, want nil without a threshold", *pricing.LongContext)
	}
}

func TestBuildBedrockMantleConfigModelsPrefersConfiguredPricing(t *testing.T) {
	entry := &config.BedrockMantleConfig{
		Profile:       "test-profile",
		DefaultRegion: "us-east-1",
		Models: []config.OpenAICompatibilityModel{
			{
				Name:    "openai.gpt-5.6-luna",
				Pricing: &registry.ModelPricing{Input: 0.1, Output: 0.2},
			},
			{Name: "openai.gpt-5.6-sol"},
		},
	}

	models := buildBedrockMantleConfigModels(entry)
	if len(models) != 2 {
		t.Fatalf("models = %d, want 2", len(models))
	}
	if models[0].Pricing == nil || models[0].Pricing.Input != 0.1 {
		t.Errorf("configured pricing = %+v, want input 0.1", models[0].Pricing)
	}
	if models[1].Pricing == nil || models[1].Pricing.Input != 4.40 {
		t.Errorf("static fallback pricing = %+v, want input 4.40", models[1].Pricing)
	}
}
