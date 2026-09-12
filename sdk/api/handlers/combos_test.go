package handlers

import (
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestComboCatalogModels(t *testing.T) {
	reg := registry.GetGlobalRegistry()
	reg.RegisterClient("test-combo-client", "openai", []*registry.ModelInfo{
		{
			ID:                  "gpt-5.4",
			ContextLength:       272000,
			MaxCompletionTokens: 128000,
			Thinking:            &registry.ThinkingSupport{Levels: []string{"low", "medium", "high"}},
		},
	})
	t.Cleanup(func() {
		reg.UnregisterClient("test-combo-client")
	})

	handler := NewBaseAPIHandlers(&sdkconfig.SDKConfig{Combos: []sdkconfig.ComboConfig{
		{Name: "coding-combo", Models: []string{"gpt-5.4"}, DisplayName: "Coding Combo"},
		{Name: "unrunnable-combo", Models: []string{"missing-model"}, DisplayName: "Unrunnable Combo"},
		{Name: "disabled", Models: []string{"gpt-5.4"}, Disabled: true},
	}}, nil)
	openAI := handler.ComboCatalogModels("openai")
	if len(openAI) != 1 || openAI[0]["id"] != "coding-combo" || openAI[0]["owned_by"] != "combo" {
		t.Fatalf("unexpected OpenAI catalog: %#v", openAI)
	}
	if openAI[0]["context_length"] != 272000 {
		t.Fatalf("expected context_length 272000, got %#v", openAI[0]["context_length"])
	}
	if openAI[0]["reasoning"] != true {
		t.Fatalf("expected reasoning true, got %#v", openAI[0]["reasoning"])
	}
	gemini := handler.ComboCatalogModels("gemini")
	if len(gemini) != 1 || gemini[0]["name"] != "coding-combo" {
		t.Fatalf("unexpected Gemini catalog: %#v", gemini)
	}
}

func TestComboFallbackEligible(t *testing.T) {
	if !comboFallbackEligible(&interfaces.ErrorMessage{StatusCode: http.StatusServiceUnavailable}) {
		t.Fatal("expected 503 to be fallback eligible")
	}
	if comboFallbackEligible(&interfaces.ErrorMessage{StatusCode: http.StatusBadRequest}) {
		t.Fatal("plain 400 must not be fallback eligible")
	}
}

func BenchmarkComboCatalogModels(b *testing.B) {
	reg := registry.GetGlobalRegistry()
	reg.RegisterClient("bench-combo-client", "openai", []*registry.ModelInfo{
		{ID: "claude-3-5-haiku", ContextLength: 200000},
		{ID: "gemini-2.0-flash", ContextLength: 1000000},
	})
	b.Cleanup(func() {
		reg.UnregisterClient("bench-combo-client")
	})

	handler := NewBaseAPIHandlers(&sdkconfig.SDKConfig{Combos: []sdkconfig.ComboConfig{
		{Name: "coding-combo", Models: []string{"claude-3-5-haiku", "gemini-2.0-flash"}, DisplayName: "Coding Combo"},
		{Name: "research-combo", Models: []string{"gemini-2.0-flash", "claude-3-5-haiku"}, DisplayName: "Research Combo"},
	}}, nil)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = handler.ComboCatalogModels("openai")
	}
}

func BenchmarkComboMemberOrder(b *testing.B) {
	combo := sdkconfig.ComboConfig{
		Name:     "round-robin-combo",
		Strategy: "round-robin",
		Models:   []string{"claude-3-5-haiku", "gemini-2.0-flash", "gpt-4o-mini"},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = comboMemberOrder(combo)
	}
}
