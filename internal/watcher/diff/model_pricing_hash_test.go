package diff

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

func TestModelSummariesTrackPricingChanges(t *testing.T) {
	basePricing := &registry.ModelPricing{Input: 3, Output: 15}
	changedPricing := &registry.ModelPricing{Input: 3.5, Output: 15}

	claudeBase := SummarizeClaudeModels([]config.ClaudeModel{{Name: "m", Alias: "a", Pricing: basePricing}})
	claudeChanged := SummarizeClaudeModels([]config.ClaudeModel{{Name: "m", Alias: "a", Pricing: changedPricing}})
	if claudeBase == claudeChanged {
		t.Error("claude summary ignores pricing changes")
	}

	codexBase := SummarizeCodexModels([]config.CodexModel{{Name: "m", Alias: "a", Pricing: basePricing}})
	codexChanged := SummarizeCodexModels([]config.CodexModel{{Name: "m", Alias: "a", Pricing: changedPricing}})
	if codexBase == codexChanged {
		t.Error("codex summary ignores pricing changes")
	}

	geminiBase := SummarizeGeminiModels([]config.GeminiModel{{Name: "m", Alias: "a", Pricing: basePricing}})
	geminiChanged := SummarizeGeminiModels([]config.GeminiModel{{Name: "m", Alias: "a", Pricing: changedPricing}})
	if geminiBase == geminiChanged {
		t.Error("gemini summary ignores pricing changes")
	}

	vertexBase := SummarizeVertexModels([]config.VertexCompatModel{{Name: "m", Alias: "a", Pricing: basePricing}})
	vertexChanged := SummarizeVertexModels([]config.VertexCompatModel{{Name: "m", Alias: "a", Pricing: changedPricing}})
	if vertexBase == vertexChanged {
		t.Error("vertex summary ignores pricing changes")
	}
}
