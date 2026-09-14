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
// item IDs and Claude reasoning content.
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

	payload := []byte(`{"model":"openai.gpt-5.6-luna","input":[` +
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

// TestBedrockMantleRetriesWithoutStalePreviousResponseID covers a session that moves
// to Mantle while the client still references a response another provider created.
func TestBedrockMantleRetriesWithoutStalePreviousResponseID(t *testing.T) {
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

	var bodies [][]byte
	roundTripper := bedrockMantleRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		body, errRead := io.ReadAll(req.Body)
		if errRead != nil {
			t.Fatalf("read upstream body: %v", errRead)
		}
		bodies = append(bodies, body)
		if len(bodies) == 1 {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"not_found_error","message":"Response not found.","type":"invalid_request_error"}}`)),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"resp_2","object":"response","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)),
		}, nil
	})
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(roundTripper))

	resp, err := exec.Execute(ctx, auth, cliproxyexecutor.Request{
		Model:   "openai.gpt-5.6-luna",
		Payload: []byte(`{"model":"openai.gpt-5.6-luna","previous_response_id":"msg_011Cf1kTSLqzDmUMB7kHWf1w","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}],"stream":false}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("upstream calls = %d, want 2", len(bodies))
	}
	if got := gjson.GetBytes(bodies[0], "previous_response_id").String(); got != "msg_011Cf1kTSLqzDmUMB7kHWf1w" {
		t.Errorf("first attempt dropped previous_response_id: %s", bodies[0])
	}
	if gjson.GetBytes(bodies[1], "previous_response_id").Exists() {
		t.Errorf("retry kept previous_response_id: %s", bodies[1])
	}
	if gjson.GetBytes(resp.Payload, "id").String() != "resp_2" {
		t.Errorf("payload = %s", resp.Payload)
	}
}
