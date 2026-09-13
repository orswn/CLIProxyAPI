package executor

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

// TestBedrockMantleResponsesInputSurvivesProviderSwitch covers a session that moves
// from a Claude model to a Mantle model. The replayed history carries repeated
// item IDs, Claude reasoning content, and a response ID Bedrock cannot resolve.
func TestBedrockMantleResponsesInputSurvivesProviderSwitch(t *testing.T) {
	cfg := &config.Config{
		SDKConfig: config.SDKConfig{
			BedrockMantle: []config.BedrockMantleConfig{{
				AccessKeyID:     "AKIA-EXAMPLE",
				SecretAccessKey: "SECRET-EXAMPLE",
				DefaultRegion:   "us-east-1",
			}},
		},
	}
	exec := NewBedrockMantleExecutor(cfg)
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"access_key_id":     "AKIA-EXAMPLE",
		"secret_access_key": "SECRET-EXAMPLE",
	}}

	var upstreamBody []byte
	roundTripper := bedrockMantleRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		var errRead error
		upstreamBody, errRead = io.ReadAll(req.Body)
		if errRead != nil {
			t.Fatalf("read upstream body: %v", errRead)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"resp_1","object":"response","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)),
		}, nil
	})
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(roundTripper))

	payload := []byte(`{"model":"openai.gpt-5.6-luna","previous_response_id":"msg_011Cf1kTSLqzDmUMB7kHWf1w","input":[` +
		`{"type":"message","id":"msg_5","role":"assistant","content":[{"type":"output_text","text":"first"}]},` +
		`{"type":"message","id":"msg_5","role":"assistant","content":[{"type":"output_text","text":"second"}]},` +
		`{"type":"reasoning","id":"rs_claude","encrypted_content":"ErUBCkYIBRgCKkDxClaudeThinkingSignature","summary":[]},` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}],"stream":false}`)

	if _, err := exec.Execute(ctx, auth, cliproxyexecutor.Request{
		Model:   "openai.gpt-5.6-luna",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if gjson.GetBytes(upstreamBody, "previous_response_id").Exists() {
		t.Errorf("previous_response_id reached Mantle: %s", upstreamBody)
	}
	items := gjson.GetBytes(upstreamBody, "input").Array()
	seen := map[string]int{}
	for _, item := range items {
		if id := item.Get("id").String(); id != "" {
			seen[id]++
		}
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("item id %q appears %d times: %s", id, count, upstreamBody)
		}
	}
	if got := items[0].Get("id").String(); got != "msg_5" {
		t.Errorf("first item lost its id: %s", items[0].Raw)
	}
	if got := items[1].Get("content.0.text").String(); got != "second" {
		t.Errorf("duplicate item lost its content: %s", items[1].Raw)
	}
	for _, item := range items {
		if item.Get("type").String() == "reasoning" && item.Get("encrypted_content").Exists() {
			t.Errorf("claude reasoning content reached Mantle: %s", item.Raw)
		}
	}
}
