package executor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestBedrockMantleRequestToFormatMatchesWireProtocol(t *testing.T) {
	exec := NewBedrockMantleExecutor(&config.Config{})
	tests := []struct {
		name   string
		source sdktranslator.Format
		want   sdktranslator.Format
	}{
		{name: "Responses", source: sdktranslator.FormatOpenAIResponse, want: sdktranslator.FormatOpenAIResponse},
		{name: "Chat Completions", source: sdktranslator.FormatOpenAI, want: sdktranslator.FormatOpenAI},
		{name: "Claude", source: sdktranslator.FormatClaude, want: sdktranslator.FormatOpenAI},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := exec.RequestToFormat(cliproxyexecutor.Request{}, cliproxyexecutor.Options{
				SourceFormat: tt.source,
			})
			if got != tt.want {
				t.Fatalf("RequestToFormat() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBedrockMantleRegionResolution(t *testing.T) {
	cfg := &config.Config{
		SDKConfig: config.SDKConfig{
			BedrockMantle: []config.BedrockMantleConfig{
				{
					Name:          "mantle-prod",
					Profile:       "boon-bedrock-codex",
					DefaultRegion: "us-east-1",
					ModelRegions: map[string]string{
						"openai.gpt-6-astra": "us-west-2",
						"gpt-6-astra":        "us-west-2",
						"claude-fable-5.1":   "us-east-2",
					},
				},
			},
		},
	}
	exec := NewBedrockMantleExecutor(cfg)
	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{
			"config_index": "0",
		},
	}

	tests := []struct {
		model      string
		wantRegion string
	}{
		{"openai.gpt-6-astra", "us-west-2"},
		{"gpt-6-astra", "us-west-2"},
		{"claude-fable-5.1", "us-east-2"},
		{"gemini-3.8-flash", "us-east-1"}, // fallback to DefaultRegion
		{"unknown-model", "us-east-1"},
	}

	for _, tt := range tests {
		got := exec.resolveRegion(auth, tt.model)
		if got != tt.wantRegion {
			t.Errorf("resolveRegion(%q) = %q, want %q", tt.model, got, tt.wantRegion)
		}
	}
}

func TestBedrockMantleExecutionSigV4(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "AWS4-HMAC-SHA256 Credential=AKIA-EXAMPLE") {
			t.Errorf("unexpected auth header: %s", authHeader)
		}
		if r.Header.Get("x-amz-date") == "" {
			t.Errorf("missing x-amz-date header")
		}
		if r.Header.Get("x-amz-content-sha256") == "" {
			t.Errorf("missing x-amz-content-sha256 header")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-test",
			"object": "chat.completion",
			"created": 123456789,
			"model": "openai.gpt-6-astra",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "content": "Hello from Mantle!"},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
		}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		SDKConfig: config.SDKConfig{
			BedrockMantle: []config.BedrockMantleConfig{
				{
					AccessKeyID:     "AKIA-EXAMPLE",
					SecretAccessKey: "SECRET-EXAMPLE",
					DefaultRegion:   "us-west-2",
					ModelRegions: map[string]string{
						"openai.gpt-6-astra": "us-west-2",
					},
				},
			},
		},
	}
	exec := NewBedrockMantleExecutor(cfg)

	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{
			"access_key_id":     "AKIA-EXAMPLE",
			"secret_access_key": "SECRET-EXAMPLE",
			"config_index":      "0",
		},
	}

	payload := []byte(`{"model":"openai.gpt-6-astra","messages":[{"role":"user","content":"Hi"}]}`)
	httpReq, err := http.NewRequest("POST", server.URL+"/v1/chat/completions", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("failed to create http request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	creds, errCreds := exec.resolveCredentials(context.Background(), auth)
	if errCreds != nil {
		t.Fatalf("resolveCredentials failed: %v", errCreds)
	}
	region := exec.resolveRegion(auth, "openai.gpt-6-astra")
	if region != "us-west-2" {
		t.Fatalf("expected region us-west-2, got %s", region)
	}

	if errSign := util.SignAwsRequest(httpReq, creds, region, "bedrock-mantle", payload, time.Now()); errSign != nil {
		t.Fatalf("SignAwsRequest failed: %v", errSign)
	}

	resp, err := exec.HttpRequest(context.Background(), auth, httpReq)
	if err != nil {
		t.Fatalf("HttpRequest failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("got status %d, want 200", resp.StatusCode)
	}
}

type bedrockMantleRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f bedrockMantleRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestBedrockMantleResponsesExecutionUsesNativeEndpoint(t *testing.T) {
	cfg := &config.Config{
		SDKConfig: config.SDKConfig{
			BedrockMantle: []config.BedrockMantleConfig{
				{
					AccessKeyID:     "AKIA-EXAMPLE",
					SecretAccessKey: "SECRET-EXAMPLE",
					DefaultRegion:   "us-east-1",
				},
			},
		},
	}
	exec := NewBedrockMantleExecutor(cfg)
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"access_key_id":     "AKIA-EXAMPLE",
		"secret_access_key": "SECRET-EXAMPLE",
	}}

	var upstreamPath string
	var upstreamBody []byte
	roundTripper := bedrockMantleRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		upstreamPath = req.URL.Path
		var errRead error
		upstreamBody, errRead = io.ReadAll(req.Body)
		if errRead != nil {
			t.Fatalf("read upstream body: %v", errRead)
		}
		if !strings.HasPrefix(req.Header.Get("Authorization"), "AWS4-HMAC-SHA256") {
			t.Fatalf("missing SigV4 authorization header")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"id":"resp_test",
				"object":"response",
				"status":"completed",
				"output":[{"type":"function_call","call_id":"call_test","name":"get_word","arguments":"{\\"value\\":\\"PONG\\"}"}],
				"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}
			}`)),
		}, nil
	})
	ctx := context.WithValue(
		context.Background(),
		"cliproxy.roundtripper",
		http.RoundTripper(roundTripper),
	)

	payload := []byte(`{
		"model":"openai.gpt-5.6-luna",
		"input":"Call get_word with value PONG.",
		"tools":[{"type":"function","name":"get_word","parameters":{"type":"object"}}],
		"reasoning":{"effort":"medium"},
		"stream":false
	}`)
	resp, err := exec.Execute(ctx, auth, cliproxyexecutor.Request{
		Model:   "openai.gpt-5.6-luna",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FormatOpenAIResponse,
		ResponseFormat:  sdktranslator.FormatOpenAIResponse,
		OriginalRequest: payload,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if upstreamPath != "/openai/v1/responses" {
		t.Fatalf("upstream path = %q, want %q", upstreamPath, "/openai/v1/responses")
	}
	if !gjson.GetBytes(upstreamBody, "input").Exists() {
		t.Fatalf("native Responses input missing from upstream body: %s", upstreamBody)
	}
	if gjson.GetBytes(upstreamBody, "messages").Exists() {
		t.Fatalf("upstream body was converted to Chat Completions: %s", upstreamBody)
	}
	if got := gjson.GetBytes(upstreamBody, "reasoning.effort").String(); got != "medium" {
		t.Fatalf("reasoning effort = %q, want medium", got)
	}
	if got := gjson.GetBytes(resp.Payload, "id").String(); got != "resp_test" {
		t.Fatalf("response id = %q, want resp_test; payload: %s", got, resp.Payload)
	}
}

func TestBedrockMantleResponsesStreamUsesNativeEndpoint(t *testing.T) {
	cfg := &config.Config{
		SDKConfig: config.SDKConfig{
			BedrockMantle: []config.BedrockMantleConfig{
				{
					AccessKeyID:     "AKIA-EXAMPLE",
					SecretAccessKey: "SECRET-EXAMPLE",
					DefaultRegion:   "us-east-1",
				},
			},
		},
	}
	exec := NewBedrockMantleExecutor(cfg)
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"access_key_id":     "AKIA-EXAMPLE",
		"secret_access_key": "SECRET-EXAMPLE",
	}}

	var upstreamPath string
	var upstreamBody []byte
	roundTripper := bedrockMantleRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		upstreamPath = req.URL.Path
		var errRead error
		upstreamBody, errRead = io.ReadAll(req.Body)
		if errRead != nil {
			t.Fatalf("read upstream body: %v", errRead)
		}
		stream := strings.Join([]string{
			"event: response.created",
			`data: {"type":"response.created","response":{"id":"resp_stream","status":"in_progress"}}`,
			"",
			"event: response.completed",
			`data: {"type":"response.completed","response":{"id":"resp_stream","status":"completed","usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}`,
			"",
		}, "\n")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(stream)),
		}, nil
	})
	ctx := context.WithValue(
		context.Background(),
		"cliproxy.roundtripper",
		http.RoundTripper(roundTripper),
	)

	payload := []byte(`{
		"model":"openai.gpt-5.6-luna",
		"input":"Say PONG.",
		"tools":[{"type":"function","name":"get_word","parameters":{"type":"object"}}],
		"reasoning":{"effort":"high"},
		"stream":true
	}`)
	result, err := exec.ExecuteStream(ctx, auth, cliproxyexecutor.Request{
		Model:   "openai.gpt-5.6-luna",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FormatOpenAIResponse,
		ResponseFormat:  sdktranslator.FormatOpenAIResponse,
		OriginalRequest: payload,
		Stream:          true,
	})
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}

	var chunks [][]byte
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk failed: %v", chunk.Err)
		}
		chunks = append(chunks, chunk.Payload)
	}
	if upstreamPath != "/openai/v1/responses" {
		t.Fatalf("upstream path = %q, want %q", upstreamPath, "/openai/v1/responses")
	}
	if gjson.GetBytes(upstreamBody, "stream_options").Exists() {
		t.Fatalf("Chat Completions stream_options leaked into Responses body: %s", upstreamBody)
	}
	if got := gjson.GetBytes(upstreamBody, "reasoning.effort").String(); got != "high" {
		t.Fatalf("reasoning effort = %q, want high", got)
	}
	joined := string(bytes.Join(chunks, []byte("\n")))
	if !strings.Contains(joined, `"type":"response.created"`) {
		t.Fatalf("native Responses events missing from stream: %s", joined)
	}
	if !strings.Contains(joined, `"type":"response.completed"`) {
		t.Fatalf("native Responses terminal event missing from stream: %s", joined)
	}
}

func TestBedrockMantleChatPayloadDoesNotDisableReasoningForOneModelVariant(t *testing.T) {
	exec := NewBedrockMantleExecutor(&config.Config{})
	payload := []byte(`{
		"model":"openai.gpt-5.6-luna",
		"messages":[{"role":"user","content":"hello"}],
		"tools":[{"type":"function","function":{"name":"get_word","parameters":{"type":"object"}}}],
		"reasoning_effort":"medium"
	}`)

	got := exec.sanitizeMantlePayload(payload, "openai.gpt-5.6-luna")
	if effort := gjson.GetBytes(got, "reasoning_effort").String(); effort != "medium" {
		t.Fatalf("reasoning_effort = %q, want medium", effort)
	}
}

func BenchmarkBedrockMantleResponsesExecution(b *testing.B) {
	cfg := &config.Config{
		SDKConfig: config.SDKConfig{
			BedrockMantle: []config.BedrockMantleConfig{
				{
					AccessKeyID:     "AKIA-EXAMPLE",
					SecretAccessKey: "SECRET-EXAMPLE",
					DefaultRegion:   "us-east-1",
				},
			},
		},
	}
	exec := NewBedrockMantleExecutor(cfg)
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"access_key_id":     "AKIA-EXAMPLE",
		"secret_access_key": "SECRET-EXAMPLE",
	}}
	payload := []byte(`{"model":"openai.gpt-5.6-luna","input":"Return PONG.","reasoning":{"effort":"low"}}`)

	benchmarks := []struct {
		name   string
		stream bool
		body   string
	}{
		{
			name: "NonStreaming",
			body: `{"id":"resp_test","object":"response","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}`,
		},
		{
			name:   "Streaming",
			stream: true,
			body:   "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"status\":\"completed\",\"usage\":{\"input_tokens\":5,\"output_tokens\":2,\"total_tokens\":7}}}\n\n",
		},
	}

	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			roundTripper := bedrockMantleRoundTripperFunc(func(*http.Request) (*http.Response, error) {
				contentType := "application/json"
				if benchmark.stream {
					contentType = "text/event-stream"
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{contentType}},
					Body:       io.NopCloser(strings.NewReader(benchmark.body)),
				}, nil
			})
			ctx := context.WithValue(
				context.Background(),
				"cliproxy.roundtripper",
				http.RoundTripper(roundTripper),
			)
			opts := cliproxyexecutor.Options{
				SourceFormat:    sdktranslator.FormatOpenAIResponse,
				ResponseFormat:  sdktranslator.FormatOpenAIResponse,
				OriginalRequest: payload,
				Stream:          benchmark.stream,
			}
			req := cliproxyexecutor.Request{Model: "openai.gpt-5.6-luna", Payload: payload}

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if !benchmark.stream {
					if _, err := exec.Execute(ctx, auth, req, opts); err != nil {
						b.Fatal(err)
					}
					continue
				}
				result, err := exec.ExecuteStream(ctx, auth, req, opts)
				if err != nil {
					b.Fatal(err)
				}
				for chunk := range result.Chunks {
					if chunk.Err != nil {
						b.Fatal(chunk.Err)
					}
				}
			}
		})
	}
}

func TestBedrockMantleCountTokens(t *testing.T) {
	cfg := &config.Config{
		SDKConfig: config.SDKConfig{
			BedrockMantle: []config.BedrockMantleConfig{
				{
					AccessKeyID:     "AKIA-EXAMPLE",
					SecretAccessKey: "SECRET-EXAMPLE",
				},
			},
		},
	}
	exec := NewBedrockMantleExecutor(cfg)
	auth := &cliproxyauth.Auth{}

	req := cliproxyexecutor.Request{
		Model:   "openai.gpt-6-astra",
		Payload: []byte(`{"model":"openai.gpt-6-astra","messages":[{"role":"user","content":"Hello world token counting"}]}`),
	}
	opts := cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatOpenAI,
	}

	resp, err := exec.CountTokens(context.Background(), auth, req, opts)
	if err != nil {
		t.Fatalf("CountTokens failed: %v", err)
	}
	if len(resp.Payload) == 0 {
		t.Fatal("expected non-empty payload from CountTokens")
	}
}
