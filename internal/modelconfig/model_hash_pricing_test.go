package modelconfig

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

func TestModelHashesTrackPricingChanges(t *testing.T) {
	basePricing := &registry.ModelPricing{Input: 3, Output: 15}
	changedPricing := &registry.ModelPricing{
		Input:       3,
		Output:      15,
		LongContext: &registry.LongContextPricing{Threshold: 200000, Input: 6, Output: 22.5},
	}

	claudeBase := ComputeClaudeModelsHash([]config.ClaudeModel{{Name: "m", Alias: "a", Pricing: basePricing}})
	claudeChanged := ComputeClaudeModelsHash([]config.ClaudeModel{{Name: "m", Alias: "a", Pricing: changedPricing}})
	if claudeBase == claudeChanged {
		t.Error("claude hash ignores pricing changes")
	}

	compatBase := ComputeOpenAICompatModelsHash([]config.OpenAICompatibilityModel{{Name: "m", Alias: "a", Pricing: basePricing}})
	compatChanged := ComputeOpenAICompatModelsHash([]config.OpenAICompatibilityModel{{Name: "m", Alias: "a", Pricing: changedPricing}})
	if compatBase == compatChanged {
		t.Error("openai-compatibility hash ignores pricing changes")
	}

	codexBase := ComputeCodexModelsHash([]config.CodexModel{{Name: "m", Alias: "a", Pricing: basePricing}})
	codexChanged := ComputeCodexModelsHash([]config.CodexModel{{Name: "m", Alias: "a", Pricing: changedPricing}})
	if codexBase == codexChanged {
		t.Error("codex hash ignores pricing changes")
	}

	geminiBase := ComputeGeminiModelsHash([]config.GeminiModel{{Name: "m", Alias: "a", Pricing: basePricing}})
	geminiChanged := ComputeGeminiModelsHash([]config.GeminiModel{{Name: "m", Alias: "a", Pricing: changedPricing}})
	if geminiBase == geminiChanged {
		t.Error("gemini hash ignores pricing changes")
	}

	vertexBase := ComputeVertexCompatModelsHash([]config.VertexCompatModel{{Name: "m", Alias: "a", Pricing: basePricing}})
	vertexChanged := ComputeVertexCompatModelsHash([]config.VertexCompatModel{{Name: "m", Alias: "a", Pricing: changedPricing}})
	if vertexBase == vertexChanged {
		t.Error("vertex hash ignores pricing changes")
	}
}
