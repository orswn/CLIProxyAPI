package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNormalizeUpstreamAPI(t *testing.T) {
	for _, testCase := range []struct {
		value string
		want  string
		ok    bool
	}{
		{value: "", want: "", ok: false},
		{value: "  ", want: "", ok: false},
		{value: "responses", want: UpstreamAPIResponses, ok: true},
		{value: " Responses ", want: UpstreamAPIResponses, ok: true},
		{value: "openai-responses", want: UpstreamAPIResponses, ok: true},
		{value: "chat-completions", want: UpstreamAPIChatCompletions, ok: true},
		{value: "chat", want: UpstreamAPIChatCompletions, ok: true},
		{value: "grpc", want: "", ok: false},
	} {
		got, ok := NormalizeUpstreamAPI(testCase.value)
		if got != testCase.want || ok != testCase.ok {
			t.Errorf("NormalizeUpstreamAPI(%q) = (%q, %t), want (%q, %t)", testCase.value, got, ok, testCase.want, testCase.ok)
		}
	}
}

func TestUpstreamAPIForModel(t *testing.T) {
	compat := &OpenAICompatibility{
		Name:        "fireworks",
		UpstreamAPI: "responses",
		Models: []OpenAICompatibilityModel{
			{Name: "accounts/fireworks/models/kimi-k3", Alias: "kimi-k3"},
			{Name: "legacy-model", Alias: "legacy", UpstreamAPI: "chat-completions"},
			{Name: "typo-model", Alias: "typo", UpstreamAPI: "grpc"},
		},
	}

	if got := compat.UpstreamAPIForModel("accounts/fireworks/models/kimi-k3", "kimi-k3"); got != UpstreamAPIResponses {
		t.Errorf("provider default = %q, want %q", got, UpstreamAPIResponses)
	}
	if got := compat.UpstreamAPIForModel("legacy-model", "legacy"); got != UpstreamAPIChatCompletions {
		t.Errorf("model override = %q, want %q", got, UpstreamAPIChatCompletions)
	}
	if got := compat.UpstreamAPIForModel("legacy", ""); got != UpstreamAPIChatCompletions {
		t.Errorf("alias override = %q, want %q", got, UpstreamAPIChatCompletions)
	}
	if got := compat.UpstreamAPIForModel("typo-model", "typo"); got != UpstreamAPIResponses {
		t.Errorf("unknown model value = %q, want provider default %q", got, UpstreamAPIResponses)
	}
	if got := compat.UpstreamAPIForModel("unknown-model"); got != UpstreamAPIResponses {
		t.Errorf("unmatched model = %q, want provider default %q", got, UpstreamAPIResponses)
	}

	plain := &OpenAICompatibility{Name: "openrouter"}
	if got := plain.UpstreamAPIForModel("any"); got != UpstreamAPIChatCompletions {
		t.Errorf("unset provider = %q, want %q", got, UpstreamAPIChatCompletions)
	}
	var missing *OpenAICompatibility
	if got := missing.UpstreamAPIForModel("any"); got != UpstreamAPIChatCompletions {
		t.Errorf("nil provider = %q, want %q", got, UpstreamAPIChatCompletions)
	}
}

func TestUpstreamAPIConfigDecoding(t *testing.T) {
	const yamlConfig = `openai-compatibility:
  - name: Fireworks
    base-url: https://api.fireworks.ai/inference/v1
    upstream-api: responses
    models:
      - name: accounts/fireworks/models/kimi-k3
        alias: kimi-k3
      - name: some-legacy-model
        alias: legacy
        upstream-api: chat-completions
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(yamlConfig), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	compat := cfg.OpenAICompatibility[0]
	if compat.UpstreamAPI != "responses" {
		t.Errorf("provider upstream-api = %q, want %q", compat.UpstreamAPI, "responses")
	}
	if compat.Models[1].UpstreamAPI != "chat-completions" {
		t.Errorf("model upstream-api = %q, want %q", compat.Models[1].UpstreamAPI, "chat-completions")
	}
	if got := compat.UpstreamAPIForModel("accounts/fireworks/models/kimi-k3"); got != UpstreamAPIResponses {
		t.Errorf("resolved provider api = %q, want %q", got, UpstreamAPIResponses)
	}
	if got := compat.UpstreamAPIForModel("some-legacy-model"); got != UpstreamAPIChatCompletions {
		t.Errorf("resolved model api = %q, want %q", got, UpstreamAPIChatCompletions)
	}
}
