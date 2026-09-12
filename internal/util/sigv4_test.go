package util

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSignAwsRequest(t *testing.T) {
	req, err := http.NewRequest("POST", "https://bedrock-mantle.us-east-1.amazonaws.com/v1/chat/completions", strings.NewReader(`{"model":"claude-fable-5.1"}`))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	creds := AwsCredentials{
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		SessionToken:    "session-token-example",
	}

	fixedTime := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	body := []byte(`{"model":"claude-fable-5.1"}`)

	if err := SignAwsRequest(req, creds, "us-east-1", "bedrock", body, fixedTime); err != nil {
		t.Fatalf("SignAwsRequest returned error: %v", err)
	}

	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20260912/us-east-1/bedrock/aws4_request") {
		t.Fatalf("unexpected Authorization header prefix: %s", auth)
	}
	if !strings.Contains(auth, "SignedHeaders=content-type;host;x-amz-content-sha256;x-amz-date;x-amz-security-token") {
		t.Fatalf("unexpected SignedHeaders: %s", auth)
	}
	if !strings.Contains(auth, "Signature=") {
		t.Fatalf("missing Signature in header: %s", auth)
	}
	if req.Header.Get("x-amz-date") != "20260912T100000Z" {
		t.Fatalf("unexpected x-amz-date: %s", req.Header.Get("x-amz-date"))
	}
	if req.Header.Get("x-amz-security-token") != "session-token-example" {
		t.Fatalf("unexpected x-amz-security-token: %s", req.Header.Get("x-amz-security-token"))
	}
}
