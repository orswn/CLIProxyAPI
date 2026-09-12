package openai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestOpenAIModelsEmitsRichMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reg := registry.GetGlobalRegistry()
	clientID := "test-rich-metadata-client"
	reg.RegisterClient(clientID, "bedrock-mantle", []*registry.ModelInfo{
		{
			ID:                        "openai.gpt-5.6-luna",
			OwnedBy:                   "bedrock-mantle",
			DisplayName:               "GPT 5.6 Luna",
			ContextLength:             1000000,
			MaxCompletionTokens:       128000,
			SupportedInputModalities:  []string{"text", "image"},
			SupportedOutputModalities: []string{"text"},
			Thinking:                  &registry.ThinkingSupport{Levels: []string{"none", "low", "medium", "high", "xhigh", "max"}},
			Pricing: &registry.ModelPricing{
				Input:         0.22,
				Output:        1.32,
				Cached:        0.022,
				CacheCreation: 0.275,
				Reasoning:     1.32,
				LongContext: &registry.LongContextPricing{
					Threshold:     272000,
					Input:         0.44,
					Output:        1.98,
					Cached:        0.044,
					CacheCreation: 0.55,
					Reasoning:     1.98,
				},
			},
			MetadataModelID: "internal-id-hidden",
		},
	})
	t.Cleanup(func() {
		reg.UnregisterClient(clientID)
	})

	cfg := &sdkconfig.SDKConfig{
		Combos: []sdkconfig.ComboConfig{
			{
				Name:        "luna-combo",
				Models:      []string{"openai.gpt-5.6-luna"},
				DisplayName: "Luna Combo",
			},
		},
	}
	base := handlers.NewBaseAPIHandlers(cfg, nil)
	handler := NewOpenAIAPIHandler(base)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/v1/models", nil)

	handler.OpenAIModels(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp struct {
		Object string           `json:"object"`
		Data   []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Object != "list" {
		t.Errorf("object = %q, want list", resp.Object)
	}

	var lunaModel map[string]any
	var comboModel map[string]any
	for _, m := range resp.Data {
		if m["id"] == "openai.gpt-5.6-luna" {
			lunaModel = m
		} else if m["id"] == "luna-combo" {
			comboModel = m
		}
	}

	if lunaModel == nil {
		t.Fatal("expected openai.gpt-5.6-luna in models list")
	}
	if comboModel == nil {
		t.Fatal("expected luna-combo in models list")
	}

	// Verify luna model metadata
	if lunaModel["context_length"] != float64(1000000) {
		t.Errorf("context_length = %v, want 1000000", lunaModel["context_length"])
	}
	if lunaModel["max_completion_tokens"] != float64(128000) {
		t.Errorf("max_completion_tokens = %v, want 128000", lunaModel["max_completion_tokens"])
	}
	if lunaModel["reasoning"] != true {
		t.Errorf("reasoning = %v, want true", lunaModel["reasoning"])
	}

	capMap, ok := lunaModel["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities is not a map: %#v", lunaModel["capabilities"])
	}
	if capMap["contextWindow"] != float64(1000000) {
		t.Errorf("capabilities.contextWindow = %v, want 1000000", capMap["contextWindow"])
	}
	if capMap["maxOutput"] != float64(128000) {
		t.Errorf("capabilities.maxOutput = %v, want 128000", capMap["maxOutput"])
	}
	if capMap["reasoning"] != true {
		t.Errorf("capabilities.reasoning = %v, want true", capMap["reasoning"])
	}
	if capMap["vision"] != true {
		t.Errorf("capabilities.vision = %v, want true", capMap["vision"])
	}

	priceMap, ok := lunaModel["pricing"].(map[string]any)
	if !ok {
		t.Fatalf("pricing is not a map: %#v", lunaModel["pricing"])
	}
	if priceMap["input"] != 0.22 {
		t.Errorf("pricing.input = %v, want 0.22", priceMap["input"])
	}
	if priceMap["output"] != 1.32 {
		t.Errorf("pricing.output = %v, want 1.32", priceMap["output"])
	}

	compatMap, ok := lunaModel["compat"].(map[string]any)
	if !ok {
		t.Fatalf("compat is not a map: %#v", lunaModel["compat"])
	}
	if compatMap["api"] != "openai-responses" {
		t.Errorf("compat.api = %v, want openai-responses", compatMap["api"])
	}

	// Verify internal fields are absent
	if _, exists := lunaModel["metadata_model_id"]; exists {
		t.Errorf("exposed internal metadata_model_id: %#v", lunaModel)
	}

	// Verify combo metadata
	if comboModel["context_length"] != float64(1000000) {
		t.Errorf("combo context_length = %v, want 1000000", comboModel["context_length"])
	}
	if comboModel["reasoning"] != true {
		t.Errorf("combo reasoning = %v, want true", comboModel["reasoning"])
	}
}
