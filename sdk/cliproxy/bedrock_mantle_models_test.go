package cliproxy

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestBuildBedrockMantleConfigModels_EmptyModelsDefaultsToCanonical(t *testing.T) {
	entry := &config.BedrockMantleConfig{
		Profile:       "test-profile",
		DefaultRegion: "us-east-1",
	}

	models := buildBedrockMantleConfigModels(entry)
	if len(models) == 0 {
		t.Fatal("expected canonical models when entry.Models is empty, got 0")
	}

	var foundLuna, foundSol, foundTerra, foundAstra bool
	for _, m := range models {
		if m.ID == "openai.gpt-5.6-luna" {
			foundLuna = true
			if m.ContextLength != 1000000 {
				t.Errorf("luna ContextLength = %d, want 1000000", m.ContextLength)
			}
			if m.MaxCompletionTokens != 128000 {
				t.Errorf("luna MaxCompletionTokens = %d, want 128000", m.MaxCompletionTokens)
			}
			if m.Pricing == nil || m.Pricing.Input != 0.22 {
				t.Errorf("luna pricing input = %v, want 0.22", m.Pricing)
			}
		} else if m.ID == "openai.gpt-5.6-sol" {
			foundSol = true
			if m.Pricing == nil || m.Pricing.Input != 4.40 {
				t.Errorf("sol pricing input = %v, want 4.40", m.Pricing)
			}
		} else if m.ID == "openai.gpt-5.6-terra" {
			foundTerra = true
		} else if m.ID == "openai.gpt-6-astra" {
			foundAstra = true
		}
	}

	if !foundLuna || !foundSol || !foundTerra || !foundAstra {
		t.Errorf("missing expected models: luna=%v sol=%v terra=%v astra=%v",
			foundLuna, foundSol, foundTerra, foundAstra)
	}
}

func TestBuildBedrockMantleConfigModels_EnrichesConfiguredModels(t *testing.T) {
	entry := &config.BedrockMantleConfig{
		Profile:       "test-profile",
		DefaultRegion: "us-east-1",
		Models: []config.OpenAICompatibilityModel{
			{
				Name: "openai.gpt-5.6-luna",
			},
			{
				Name:  "openai.gpt-5.6-sol",
				Alias: "sol-fast",
			},
		},
	}

	models := buildBedrockMantleConfigModels(entry)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}

	m0 := models[0]
	if m0.ID != "openai.gpt-5.6-luna" {
		t.Errorf("m0 ID = %q, want openai.gpt-5.6-luna", m0.ID)
	}
	if m0.ContextLength != 1000000 {
		t.Errorf("m0 ContextLength = %d, want 1000000", m0.ContextLength)
	}
	if m0.Thinking == nil || len(m0.Thinking.Levels) == 0 {
		t.Error("m0 expected Thinking levels from canonical metadata")
	}
	if m0.Pricing == nil || m0.Pricing.Input != 0.22 {
		t.Errorf("m0 Pricing input = %v, want 0.22", m0.Pricing)
	}

	m1 := models[1]
	if m1.ID != "sol-fast" {
		t.Errorf("m1 ID = %q, want sol-fast", m1.ID)
	}
	if m1.ContextLength != 1000000 {
		t.Errorf("m1 ContextLength = %d, want 1000000", m1.ContextLength)
	}
	if m1.Pricing == nil || m1.Pricing.Input != 4.40 {
		t.Errorf("m1 Pricing input = %v, want 4.40", m1.Pricing)
	}
}
