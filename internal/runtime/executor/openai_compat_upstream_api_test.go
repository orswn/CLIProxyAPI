package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

func newUpstreamAPITestAuth(baseURL string) *cliproxyauth.Auth {
	return &cliproxyauth.Auth{
		Provider: "openai-compatibility",
		Attributes: map[string]string{
			"base_url":     baseURL + "/v1",
			"api_key":      "test",
			"compat_name":  "compat",
			"provider_key": "compat",
		},
	}
}

func newUpstreamAPITestExecutor(upstreamAPI string, models []config.OpenAICompatibilityModel) *OpenAICompatExecutor {
	return NewOpenAICompatExecutor("openai-compatibility", &config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{{
			Name:        "compat",
			UpstreamAPI: upstreamAPI,
			Models:      models,
		}},
	})
}

func TestOpenAICompatExecutorResponsesUpstreamNonStream(t *testing.T) {
	var gotPath string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","status":"completed","output":[],"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}`))
	}))
	defer server.Close()

	executor := newUpstreamAPITestExecutor("responses", []config.OpenAICompatibilityModel{{
		Name:  "accounts/fireworks/models/kimi-k3",
		Alias: "kimi-k3",
	}})
	resp, err := executor.Execute(context.Background(), newUpstreamAPITestAuth(server.URL), cliproxyexecutor.Request{
		Model:   "accounts/fireworks/models/kimi-k3",
		Payload: []byte(`{"model":"kimi-k3","input":[{"role":"user","content":"hi"}],"stream":true}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatOpenAIResponse,
		Stream:       false,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if gotPath != "/v1/responses" {
		t.Fatalf("path = %q, want %q", gotPath, "/v1/responses")
	}
	if !gjson.GetBytes(gotBody, "input").Exists() {
		t.Fatalf("expected input in body: %s", string(gotBody))
	}
	if gjson.GetBytes(gotBody, "messages").Exists() {
		t.Fatalf("unexpected messages in body: %s", string(gotBody))
	}
	if gjson.GetBytes(gotBody, "stream").Exists() {
		t.Fatalf("unexpected stream flag in non-stream body: %s", string(gotBody))
	}
	if gjson.GetBytes(gotBody, "model").String() != "accounts/fireworks/models/kimi-k3" {
		t.Fatalf("model = %s", string(gotBody))
	}
	if gjson.GetBytes(resp.Payload, "id").String() != "resp_1" {
		t.Fatalf("payload = %s", string(resp.Payload))
	}
}

func TestOpenAICompatExecutorDefaultsToChatCompletions(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	executor := newUpstreamAPITestExecutor("", []config.OpenAICompatibilityModel{{Name: "kimi-k3", Alias: "kimi-k3"}})
	if _, err := executor.Execute(context.Background(), newUpstreamAPITestAuth(server.URL), cliproxyexecutor.Request{
		Model:   "kimi-k3",
		Payload: []byte(`{"model":"kimi-k3","input":[{"role":"user","content":"hi"}]}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatOpenAIResponse,
	}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q, want %q", gotPath, "/v1/chat/completions")
	}
}

func TestOpenAICompatExecutorModelOverridesProviderUpstreamAPI(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	executor := newUpstreamAPITestExecutor("responses", []config.OpenAICompatibilityModel{{
		Name:        "some-legacy-model",
		Alias:       "legacy",
		UpstreamAPI: "chat-completions",
	}})
	if _, err := executor.Execute(context.Background(), newUpstreamAPITestAuth(server.URL), cliproxyexecutor.Request{
		Model:   "some-legacy-model",
		Payload: []byte(`{"model":"legacy","input":[{"role":"user","content":"hi"}]}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatOpenAIResponse,
	}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q, want %q", gotPath, "/v1/chat/completions")
	}
}

func TestOpenAICompatExecutorResponsesUpstreamKeepsChatForUnsupportedSchema(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	executor := newUpstreamAPITestExecutor("responses", []config.OpenAICompatibilityModel{{Name: "kimi-k3", Alias: "kimi-k3"}})
	if _, err := executor.Execute(context.Background(), newUpstreamAPITestAuth(server.URL), cliproxyexecutor.Request{
		Model:   "kimi-k3",
		Payload: []byte(`{"model":"kimi-k3","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatGemini,
	}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q, want %q for a client schema without a Responses translator", gotPath, "/v1/chat/completions")
	}
}

func TestOpenAICompatExecutorResponsesUpstreamStream(t *testing.T) {
	var gotPath string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		frames := []string{
			"event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n\n",
			"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n",
			"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"usage\":{\"input_tokens\":5,\"output_tokens\":6,\"total_tokens\":11}}}\n\n",
		}
		for _, frame := range frames {
			_, _ = w.Write([]byte(frame))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer server.Close()

	executor := newUpstreamAPITestExecutor("responses", []config.OpenAICompatibilityModel{{Name: "kimi-k3", Alias: "kimi-k3"}})
	result, err := executor.ExecuteStream(context.Background(), newUpstreamAPITestAuth(server.URL), cliproxyexecutor.Request{
		Model:   "kimi-k3",
		Payload: []byte(`{"model":"kimi-k3","input":[{"role":"user","content":"hi"}],"stream":true}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatOpenAIResponse,
		Stream:       true,
	})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}
	if gotPath != "/v1/responses" {
		t.Fatalf("path = %q, want %q", gotPath, "/v1/responses")
	}
	if gjson.GetBytes(gotBody, "stream_options").Exists() {
		t.Fatalf("unexpected stream_options in responses body: %s", string(gotBody))
	}
	if !gjson.GetBytes(gotBody, "stream").Bool() {
		t.Fatalf("expected stream flag in responses body: %s", string(gotBody))
	}

	var payloads []string
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		payloads = append(payloads, string(chunk.Payload))
	}
	if len(payloads) != 3 {
		t.Fatalf("chunks = %d (%v), want 3", len(payloads), payloads)
	}
	joined := strings.Join(payloads, "\n")
	if !strings.Contains(joined, "response.completed") {
		t.Fatalf("terminal event missing: %s", joined)
	}
	if strings.Contains(joined, "[DONE]") {
		t.Fatalf("unexpected [DONE] sentinel for a Responses upstream: %s", joined)
	}
}

func TestOpenAICompatExecutorResponsesUpstreamStreamWithoutTerminalEventFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n"))
	}))
	defer server.Close()

	executor := newUpstreamAPITestExecutor("responses", []config.OpenAICompatibilityModel{{Name: "kimi-k3", Alias: "kimi-k3"}})
	result, err := executor.ExecuteStream(context.Background(), newUpstreamAPITestAuth(server.URL), cliproxyexecutor.Request{
		Model:   "kimi-k3",
		Payload: []byte(`{"model":"kimi-k3","input":[{"role":"user","content":"hi"}],"stream":true}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatOpenAIResponse,
		Stream:       true,
	})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}
	var streamErr error
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			streamErr = chunk.Err
		}
	}
	if streamErr == nil {
		t.Fatal("expected a stream error when the upstream closes without a terminal event")
	}
	if !strings.Contains(streamErr.Error(), "terminal Responses event") {
		t.Fatalf("stream error = %v", streamErr)
	}
}

func TestOpenAICompatExecutorResponsesUpstreamTranslatesClaudeClient(t *testing.T) {
	var gotPath string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","status":"completed","model":"kimi-k3","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}`))
	}))
	defer server.Close()

	executor := newUpstreamAPITestExecutor("responses", []config.OpenAICompatibilityModel{{Name: "kimi-k3", Alias: "kimi-k3"}})
	resp, err := executor.Execute(context.Background(), newUpstreamAPITestAuth(server.URL), cliproxyexecutor.Request{
		Model:   "kimi-k3",
		Payload: []byte(`{"model":"kimi-k3","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatClaude,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if gotPath != "/v1/responses" {
		t.Fatalf("path = %q, want %q", gotPath, "/v1/responses")
	}
	if !gjson.GetBytes(gotBody, "input").Exists() {
		t.Fatalf("expected Responses input in body: %s", string(gotBody))
	}
	if gjson.GetBytes(gotBody, "messages").Exists() {
		t.Fatalf("unexpected Chat Completions messages in body: %s", string(gotBody))
	}
	if gjson.GetBytes(resp.Payload, "type").String() != "message" {
		t.Fatalf("client payload was not translated back to Claude: %s", string(resp.Payload))
	}
}

func TestOpenAICompatExecutorResponsesUpstreamDedupesInputItemIDs(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	executor := newUpstreamAPITestExecutor("responses", []config.OpenAICompatibilityModel{{Name: "kimi-k3", Alias: "kimi-k3"}})
	if _, err := executor.Execute(context.Background(), newUpstreamAPITestAuth(server.URL), cliproxyexecutor.Request{
		Model: "kimi-k3",
		Payload: []byte(`{"model":"kimi-k3","input":[` +
			`{"type":"message","id":"msg_5","role":"assistant","content":[{"type":"output_text","text":"first"}]},` +
			`{"type":"message","id":"msg_5","role":"assistant","content":[{"type":"output_text","text":"second"}]},` +
			`{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	items := gjson.GetBytes(gotBody, "input").Array()
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3: %s", len(items), gotBody)
	}
	if items[0].Get("id").String() != "msg_5" {
		t.Errorf("first item lost its id: %s", items[0].Raw)
	}
	if items[1].Get("id").Exists() {
		t.Errorf("duplicate id reached the upstream: %s", items[1].Raw)
	}
}
