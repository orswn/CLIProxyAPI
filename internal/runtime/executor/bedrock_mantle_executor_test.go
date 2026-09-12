package executor

import (
	"context"
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
)

func TestBedrockMantleRegionResolution(t *testing.T) {
	cfg := &config.Config{
		SDKConfig: config.SDKConfig{
			BedrockMantle: []config.BedrockMantleConfig{
				{
					Name:            "mantle-prod",
					Profile:         "boon-bedrock-codex",
					DefaultRegion:   "us-east-1",
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
