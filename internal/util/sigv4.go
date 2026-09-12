package util

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type AwsCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

func Sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func HmacSha256(key []byte, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func HmacSha256Hex(key []byte, data []byte) string {
	return hex.EncodeToString(HmacSha256(key, data))
}

func awsUriEncode(val string) string {
	encoded := url.QueryEscape(val)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	return encoded
}

func canonicalURI(path string) string {
	if path == "" || path == "/" {
		return "/"
	}
	parts := strings.Split(path, "/")
	for i, part := range parts {
		parts[i] = awsUriEncode(part)
	}
	return strings.Join(parts, "/")
}

func canonicalQueryString(query url.Values) string {
	if len(query) == 0 {
		return ""
	}
	type param struct {
		key   string
		value string
	}
	var params []param
	for k, vals := range query {
		for _, v := range vals {
			params = append(params, param{key: awsUriEncode(k), value: awsUriEncode(v)})
		}
	}
	sort.Slice(params, func(i, j int) bool {
		if params[i].key == params[j].key {
			return params[i].value < params[j].value
		}
		return params[i].key < params[j].key
	})
	var parts []string
	for _, p := range params {
		parts = append(parts, fmt.Sprintf("%s=%s", p.key, p.value))
	}
	return strings.Join(parts, "&")
}

// SignAwsRequest signs an HTTP request with AWS SigV4.
func SignAwsRequest(req *http.Request, creds AwsCredentials, region, service string, body []byte, now time.Time) error {
	if req == nil {
		return fmt.Errorf("request cannot be nil")
	}
	if creds.AccessKeyID == "" || creds.SecretAccessKey == "" {
		return fmt.Errorf("aws credentials missing access key id or secret access key")
	}
	if region == "" {
		region = "us-east-1"
	}
	if service == "" {
		service = "bedrock"
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")
	payloadHash := Sha256Hex(body)

	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	if creds.SessionToken != "" {
		req.Header.Set("x-amz-security-token", creds.SessionToken)
	}

	host := req.URL.Host
	if host == "" {
		host = req.Host
	}

	headerMap := make(map[string]string)
	headerMap["host"] = host
	headerMap["x-amz-date"] = amzDate
	headerMap["x-amz-content-sha256"] = payloadHash
	if creds.SessionToken != "" {
		headerMap["x-amz-security-token"] = creds.SessionToken
	}
	if ct := req.Header.Get("Content-Type"); ct != "" {
		headerMap["content-type"] = strings.TrimSpace(ct)
	}

	var signedHeaderKeys []string
	for k := range headerMap {
		signedHeaderKeys = append(signedHeaderKeys, k)
	}
	sort.Strings(signedHeaderKeys)

	var canonicalHeaders strings.Builder
	for _, k := range signedHeaderKeys {
		canonicalHeaders.WriteString(fmt.Sprintf("%s:%s\n", k, headerMap[k]))
	}
	signedHeaders := strings.Join(signedHeaderKeys, ";")

	canonicalReq := strings.Join([]string{
		req.Method,
		canonicalURI(req.URL.Path),
		canonicalQueryString(req.URL.Query()),
		canonicalHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, region, service)
	canonicalReqHash := Sha256Hex([]byte(canonicalReq))

	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		canonicalReqHash,
	}, "\n")

	kDate := HmacSha256([]byte("AWS4"+creds.SecretAccessKey), []byte(dateStamp))
	kRegion := HmacSha256(kDate, []byte(region))
	kService := HmacSha256(kRegion, []byte(service))
	signingKey := HmacSha256(kService, []byte("aws4_request"))

	signature := HmacSha256Hex(signingKey, []byte(stringToSign))

	authHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		creds.AccessKeyID, credentialScope, signedHeaders, signature)
	req.Header.Set("Authorization", authHeader)
	return nil
}
