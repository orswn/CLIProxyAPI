package handlers

import (
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestComboCatalogModels(t *testing.T) {
	handler := NewBaseAPIHandlers(&sdkconfig.SDKConfig{Combos: []sdkconfig.ComboConfig{
		{Name: "coding-combo", Models: []string{"gpt-5.4"}, DisplayName: "Coding Combo"},
		{Name: "disabled", Models: []string{"gpt-5.4"}, Disabled: true},
	}}, nil)
	openAI := handler.ComboCatalogModels("openai")
	if len(openAI) != 1 || openAI[0]["id"] != "coding-combo" || openAI[0]["owned_by"] != "combo" {
		t.Fatalf("unexpected OpenAI catalog: %#v", openAI)
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
