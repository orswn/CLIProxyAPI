package thinking_test

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

func TestClaudeOpus55ThinkingConversion(t *testing.T) {
	tests := []struct {
		name         string
		model        string
		body         string
		wantType     string
		wantEffort   string
		wantThinking bool
	}{
		{name: "disabled", model: "claude-opus-5-5", body: `{"thinking":{"type":"disabled"}}`, wantType: "adaptive", wantEffort: "low", wantThinking: true},
		{name: "suffix none", model: "claude-opus-5-5(none)", body: `{}`, wantType: "adaptive", wantEffort: "low", wantThinking: true},
		{name: "numeric zero", model: "claude-opus-5-5", body: `{"thinking":{"type":"enabled","budget_tokens":0}}`, wantType: "adaptive", wantEffort: "low", wantThinking: true},
		{name: "manual budget", model: "claude-opus-5-5", body: `{"thinking":{"type":"enabled","budget_tokens":8192}}`, wantType: "adaptive", wantEffort: "medium", wantThinking: true},
		{name: "manual without budget", model: "claude-opus-5-5", body: `{"thinking":{"type":"enabled"}}`, wantType: "adaptive", wantThinking: true},
		{name: "adaptive xhigh", model: "claude-opus-5-5", body: `{"thinking":{"type":"adaptive"},"output_config":{"effort":"xhigh"}}`, wantType: "adaptive", wantEffort: "xhigh", wantThinking: true},
		{name: "omitted", model: "claude-opus-5-5", body: `{}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			out, err := thinking.ApplyThinking([]byte(test.body), test.model, "claude", "claude", "claude")
			if err != nil {
				t.Fatalf("ApplyThinking() error = %v", err)
			}
			thinkingType := gjson.GetBytes(out, "thinking.type")
			if thinkingType.Exists() != test.wantThinking {
				t.Fatalf("thinking presence = %v, want %v; body=%s", thinkingType.Exists(), test.wantThinking, out)
			}
			if got := thinkingType.String(); got != test.wantType {
				t.Fatalf("thinking.type = %q, want %q; body=%s", got, test.wantType, out)
			}
			if got := gjson.GetBytes(out, "output_config.effort").String(); got != test.wantEffort {
				t.Fatalf("output_config.effort = %q, want %q; body=%s", got, test.wantEffort, out)
			}
			if gjson.GetBytes(out, "thinking.budget_tokens").Exists() {
				t.Fatalf("thinking.budget_tokens must be absent; body=%s", out)
			}
		})
	}
}
