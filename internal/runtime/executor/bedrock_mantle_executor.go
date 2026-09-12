package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// BedrockMantleExecutor implements execution against AWS Bedrock Mantle
// OpenAI-compatible chat completion endpoints signed with AWS SigV4.
type BedrockMantleExecutor struct {
	cfg           *config.Config
	credMu        sync.RWMutex
	credProviders map[string]aws.CredentialsProvider
}

var mantleTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          200,
	MaxIdleConnsPerHost:   50,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
}

func (e *BedrockMantleExecutor) getHTTPClient(ctx context.Context, auth *cliproxyauth.Auth) *http.Client {
	client := helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	if client.Transport == nil {
		client.Transport = mantleTransport
	}
	return client
}

// NewBedrockMantleExecutor creates an executor for Bedrock Mantle.
func NewBedrockMantleExecutor(cfg *config.Config) *BedrockMantleExecutor {
	return &BedrockMantleExecutor{
		cfg:           cfg,
		credProviders: make(map[string]aws.CredentialsProvider),
	}
}

// Identifier implements cliproxyauth.ProviderExecutor.
func (e *BedrockMantleExecutor) Identifier() string { return "bedrock-mantle" }

func (e *BedrockMantleExecutor) resolveCredentials(ctx context.Context, auth *cliproxyauth.Auth) (util.AwsCredentials, error) {
	var profile, ak, sk, st string
	if auth != nil && auth.Attributes != nil {
		profile = strings.TrimSpace(auth.Attributes["profile"])
		ak = strings.TrimSpace(auth.Attributes["access_key_id"])
		sk = strings.TrimSpace(auth.Attributes["secret_access_key"])
		st = strings.TrimSpace(auth.Attributes["session_token"])
	}
	if profile == "" && ak == "" && e.cfg != nil && len(e.cfg.BedrockMantle) > 0 {
		profile = strings.TrimSpace(e.cfg.BedrockMantle[0].Profile)
		ak = strings.TrimSpace(e.cfg.BedrockMantle[0].AccessKeyID)
		sk = strings.TrimSpace(e.cfg.BedrockMantle[0].SecretAccessKey)
		st = strings.TrimSpace(e.cfg.BedrockMantle[0].SessionToken)
	}

	if ak != "" && sk != "" {
		return util.AwsCredentials{
			AccessKeyID:     ak,
			SecretAccessKey: sk,
			SessionToken:    st,
		}, nil
	}

	cacheKey := profile
	if cacheKey == "" {
		cacheKey = "__default__"
	}

	e.credMu.RLock()
	provider := e.credProviders[cacheKey]
	e.credMu.RUnlock()

	if provider == nil {
		e.credMu.Lock()
		provider = e.credProviders[cacheKey]
		if provider == nil {
			var opts []func(*awsconfig.LoadOptions) error
			if profile != "" {
				opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
			}
			awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
			if err != nil {
				e.credMu.Unlock()
				return util.AwsCredentials{}, fmt.Errorf("bedrock mantle aws config load failed: %w", err)
			}
			provider = awsCfg.Credentials
			if e.credProviders == nil {
				e.credProviders = make(map[string]aws.CredentialsProvider)
			}
			e.credProviders[cacheKey] = provider
		}
		e.credMu.Unlock()
	}

	creds, err := provider.Retrieve(ctx)
	if err != nil {
		return util.AwsCredentials{}, fmt.Errorf("bedrock mantle credentials retrieve failed: %w", err)
	}
	return util.AwsCredentials{
		AccessKeyID:     creds.AccessKeyID,
		SecretAccessKey: creds.SecretAccessKey,
		SessionToken:    creds.SessionToken,
	}, nil
}

func (e *BedrockMantleExecutor) resolveMantleConfig(auth *cliproxyauth.Auth) *config.BedrockMantleConfig {
	if e == nil || e.cfg == nil || len(e.cfg.BedrockMantle) == 0 {
		return nil
	}
	if auth != nil && auth.Attributes != nil {
		if idxStr, ok := auth.Attributes["config_index"]; ok {
			if idx, err := strconv.Atoi(strings.TrimSpace(idxStr)); err == nil && idx >= 0 && idx < len(e.cfg.BedrockMantle) {
				return &e.cfg.BedrockMantle[idx]
			}
		}
	}
	return &e.cfg.BedrockMantle[0]
}

func (e *BedrockMantleExecutor) resolveRegion(auth *cliproxyauth.Auth, modelName string) string {
	mantleConfig := e.resolveMantleConfig(auth)
	if mantleConfig != nil {
		return mantleConfig.ResolveRegion(modelName)
	}
	return "us-east-1"
}

func (e *BedrockMantleExecutor) sanitizeMantlePayload(payload []byte, modelName string) []byte {
	m := strings.ToLower(modelName)
	if strings.Contains(m, "gpt-6") || strings.Contains(m, "gpt-5.6") || strings.Contains(m, "astra") {
		payload, _ = sjson.DeleteBytes(payload, "max_tokens")
		payload, _ = sjson.DeleteBytes(payload, "max_output_tokens")
	}
	if strings.Contains(m, "luna") {
		if gjson.GetBytes(payload, "tools").Exists() || gjson.GetBytes(payload, "functions").Exists() {
			payload, _ = sjson.SetBytes(payload, "reasoning_effort", "none")
		}
	}
	return payload
}

// PrepareRequest does not perform signing because AWS SigV4 requires the request payload hash.
func (e *BedrockMantleExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	return nil
}

// HttpRequest injects AWS SigV4 credentials into the request and executes it.
func (e *BedrockMantleExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("bedrock mantle executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	httpReq := req.WithContext(ctx)
	httpClient := e.getHTTPClient(ctx, auth)
	return httpClient.Do(httpReq)
}

func (e *BedrockMantleExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
	ctx = helps.EnsureSessionContext(ctx, opts, req.Payload)
	baseModel := thinking.ParseSuffix(req.Model).ModelName

	reporter := helps.NewExecutorUsageReporter(ctx, e, baseModel, auth)
	defer reporter.TrackFailure(ctx, &err)

	creds, errCreds := e.resolveCredentials(ctx, auth)
	if errCreds != nil {
		err = statusErr{code: http.StatusUnauthorized, msg: fmt.Sprintf("bedrock mantle credentials failed: %v", errCreds)}
		return
	}
	if creds.AccessKeyID == "" || creds.SecretAccessKey == "" {
		err = statusErr{code: http.StatusUnauthorized, msg: "missing bedrock mantle aws credentials"}
		return
	}

	region := e.resolveRegion(auth, baseModel)
	endpointURL := fmt.Sprintf("https://bedrock-mantle.%s.api.aws/openai/v1/chat/completions", region)

	from := opts.SourceFormat
	responseFormat := cliproxyexecutor.ResponseFormatOrSource(opts)
	to := sdktranslator.FromString("openai")

	originalPayloadSource := req.Payload
	if len(opts.OriginalRequest) > 0 {
		originalPayloadSource = opts.OriginalRequest
	}
	originalPayload := originalPayloadSource
	isCompat := helps.APIKeyModelIsCompat(req)
	originalTranslated := helps.TranslateRequestWithAPIKeyModelCompatibility(ctx, opts.Headers, e.cfg, from, to, baseModel, originalPayload, opts.Stream, isCompat)
	translated := helps.TranslateRequestWithAPIKeyModelCompatibility(ctx, opts.Headers, e.cfg, from, to, baseModel, req.Payload, opts.Stream, isCompat)

	translated, err = helps.ApplyRequestThinking(translated, req, opts, from.String(), to.String(), e.Identifier())
	if err != nil {
		return resp, err
	}

	requestedModel := helps.PayloadRequestedModel(opts, req.Model)
	requestPath := helps.PayloadRequestPath(opts)
	translated = helps.ApplyPayloadConfigWithRequest(e.cfg, baseModel, to.String(), from.String(), "", translated, originalTranslated, requestedModel, requestPath, opts.Headers)
	translated = e.sanitizeMantlePayload(translated, baseModel)
	reporter.SetTranslatedReasoningEffort(translated, to.String())

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(translated))
	if err != nil {
		return resp, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "cli-proxy-bedrock-mantle")

	if errSign := util.SignAwsRequest(httpReq, creds, region, "bedrock-mantle", translated, time.Now()); errSign != nil {
		return resp, fmt.Errorf("bedrock mantle sigv4 signing failed: %w", errSign)
	}

	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}
	helps.RecordAPIRequest(ctx, e.cfg, helps.UpstreamRequestLog{
		URL:       endpointURL,
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      translated,
		Provider:  e.Identifier(),
		AuthID:    authID,
		AuthLabel: authLabel,
		AuthType:  authType,
		AuthValue: authValue,
	})

	httpClient := e.getHTTPClient(ctx, auth)
	httpClient = reporter.TrackHTTPClient(httpClient)
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, err)
		return resp, err
	}
	defer func() {
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("bedrock mantle executor: close response body error: %v", errClose)
		}
	}()

	helps.RecordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(httpResp.Body)
		helps.AppendAPIResponseChunk(ctx, e.cfg, b)
		helps.LogWithRequestID(ctx).Debugf("bedrock mantle request error, status: %d, message: %s", httpResp.StatusCode, string(b))
		err = newOpenAICompatStatusError(httpResp.StatusCode, httpResp.Header, b)
		return resp, err
	}

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, err)
		return resp, err
	}
	helps.AppendAPIResponseChunk(ctx, e.cfg, body)
	reporter.Publish(ctx, helps.ParseOpenAIUsage(body))
	reporter.EnsurePublished(ctx)

	var param any
	out := sdktranslator.TranslateNonStream(ctx, to, responseFormat, req.Model, opts.OriginalRequest, translated, body, &param)
	if responseFormat == sdktranslator.FormatOpenAIResponse {
		out = helps.EnsureResponsesUsageDetails(out)
	}
	resp = cliproxyexecutor.Response{Payload: out, Headers: httpResp.Header.Clone()}
	return resp, nil
}

func (e *BedrockMantleExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (_ *cliproxyexecutor.StreamResult, err error) {
	ctx = helps.EnsureSessionContext(ctx, opts, req.Payload)
	baseModel := thinking.ParseSuffix(req.Model).ModelName

	reporter := helps.NewExecutorUsageReporter(ctx, e, baseModel, auth)
	defer reporter.TrackFailure(ctx, &err)

	creds, errCreds := e.resolveCredentials(ctx, auth)
	if errCreds != nil {
		err = statusErr{code: http.StatusUnauthorized, msg: fmt.Sprintf("bedrock mantle credentials failed: %v", errCreds)}
		return nil, err
	}
	if creds.AccessKeyID == "" || creds.SecretAccessKey == "" {
		err = statusErr{code: http.StatusUnauthorized, msg: "missing bedrock mantle aws credentials"}
		return nil, err
	}

	region := e.resolveRegion(auth, baseModel)
	endpointURL := fmt.Sprintf("https://bedrock-mantle.%s.api.aws/openai/v1/chat/completions", region)

	from := opts.SourceFormat
	responseFormat := cliproxyexecutor.ResponseFormatOrSource(opts)
	to := sdktranslator.FromString("openai")

	originalPayloadSource := req.Payload
	if len(opts.OriginalRequest) > 0 {
		originalPayloadSource = opts.OriginalRequest
	}
	originalPayload := originalPayloadSource
	isCompat := helps.APIKeyModelIsCompat(req)
	originalTranslated := helps.TranslateRequestWithAPIKeyModelCompatibility(ctx, opts.Headers, e.cfg, from, to, baseModel, originalPayload, true, isCompat)
	translated := helps.TranslateRequestWithAPIKeyModelCompatibility(ctx, opts.Headers, e.cfg, from, to, baseModel, req.Payload, true, isCompat)

	translated, err = helps.ApplyRequestThinking(translated, req, opts, from.String(), to.String(), e.Identifier())
	if err != nil {
		return nil, err
	}

	requestedModel := helps.PayloadRequestedModel(opts, req.Model)
	requestPath := helps.PayloadRequestPath(opts)
	translated = helps.ApplyPayloadConfigWithRequest(e.cfg, baseModel, to.String(), from.String(), "", translated, originalTranslated, requestedModel, requestPath, opts.Headers)
	translated = e.sanitizeMantlePayload(translated, baseModel)

	// Ensure stream_options.include_usage = true
	translated = helps.SetBoolIfDifferent(translated, "stream_options.include_usage", true)
	reporter.SetTranslatedReasoningEffort(translated, to.String())

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(translated))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Cache-Control", "no-cache")
	httpReq.Header.Set("User-Agent", "cli-proxy-bedrock-mantle")

	if errSign := util.SignAwsRequest(httpReq, creds, region, "bedrock-mantle", translated, time.Now()); errSign != nil {
		return nil, fmt.Errorf("bedrock mantle sigv4 signing failed: %w", errSign)
	}

	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}
	helps.RecordAPIRequest(ctx, e.cfg, helps.UpstreamRequestLog{
		URL:       endpointURL,
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      translated,
		Provider:  e.Identifier(),
		AuthID:    authID,
		AuthLabel: authLabel,
		AuthType:  authType,
		AuthValue: authValue,
	})

	httpClient := e.getHTTPClient(ctx, auth)
	httpClient = reporter.TrackHTTPClient(httpClient)
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, err)
		return nil, err
	}
	helps.RecordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(httpResp.Body)
		helps.AppendAPIResponseChunk(ctx, e.cfg, b)
		helps.LogWithRequestID(ctx).Debugf("bedrock mantle request error, status: %d, message: %s", httpResp.StatusCode, string(b))
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("bedrock mantle executor: close response body error: %v", errClose)
		}
		err = newOpenAICompatStatusError(httpResp.StatusCode, httpResp.Header, b)
		return nil, err
	}

	out := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		defer close(out)
		defer func() {
			if errClose := httpResp.Body.Close(); errClose != nil {
				log.Errorf("bedrock mantle executor: close response body error: %v", errClose)
			}
		}()
		scanner := bufio.NewScanner(httpResp.Body)
		scanner.Buffer(nil, 52_428_800)
		claudeInputTokens := helps.NewClaudeInputTokenState(from, to, responseFormat, originalPayload)
		var param any
		var streamUsage helps.StreamUsageBuffer
		var seenDone bool
		var streamFailed bool
		var streamAborted bool
		var upstreamEvent string
		var frameData [][]byte
		defer streamUsage.Publish(ctx, reporter)

		publishStreamError := func(streamErr statusErr, containsPayload bool) {
			loggedErr := streamErr
			if containsPayload {
				loggedErr = statusErr{code: streamErr.code, msg: "upstream stream returned an error payload"}
			}
			helps.RecordAPIResponseError(ctx, e.cfg, loggedErr)
			reporter.PublishFailure(ctx, loggedErr)
			select {
			case out <- cliproxyexecutor.StreamChunk{Err: streamErr}:
			case <-ctx.Done():
			}
			streamFailed = true
		}

		processFrame := func() bool {
			eventName := upstreamEvent
			upstreamEvent = ""
			dataLines := frameData
			frameData = nil
			if len(dataLines) == 0 {
				if openAICompatErrorEvent(eventName) {
					publishStreamError(statusErr{code: http.StatusBadGateway, msg: "upstream error event ended without data"}, false)
					return true
				}
				return false
			}

			if len(dataLines) > 1 {
				for _, dataLine := range dataLines {
					if bytes.Equal(bytes.TrimSpace(dataLine), []byte("[DONE]")) {
						publishStreamError(statusErr{code: http.StatusBadGateway, msg: "upstream stream ended with incomplete data before [DONE]"}, false)
						return true
					}
				}
			}
			dataPayload := bytes.TrimSpace(bytes.Join(dataLines, []byte("\n")))
			isDone := bytes.Equal(dataPayload, []byte("[DONE]"))
			if isDone && openAICompatErrorEvent(eventName) {
				publishStreamError(statusErr{code: http.StatusBadGateway, msg: "upstream error event ended before [DONE]"}, false)
				return true
			}
			if !isDone && !json.Valid(dataPayload) {
				publishStreamError(statusErr{code: http.StatusBadGateway, msg: "upstream stream ended with incomplete SSE data frame"}, false)
				return true
			}
			if !isDone {
				if streamErr, isError := openAICompatStreamDataError(dataPayload, eventName); isError {
					publishStreamError(streamErr, true)
					return true
				}
			}

			streamLine := append([]byte("data: "), dataPayload...)
			chunks := helps.TranslateStreamWithClaudeInputTokens(ctx, to, responseFormat, req.Model, opts.OriginalRequest, translated, streamLine, &param, claudeInputTokens)
			for i := range chunks {
				select {
				case out <- cliproxyexecutor.StreamChunk{Payload: chunks[i]}:
				case <-ctx.Done():
					streamAborted = true
					return true
				}
			}
			if isDone {
				seenDone = true
				return true
			}
			return false
		}

	scanLoop:
		for scanner.Scan() {
			line := scanner.Bytes()
			helps.AppendAPIResponseChunk(ctx, e.cfg, line)
			streamUsage.ObserveOpenAIStream(line)
			trimmedLine := bytes.TrimSpace(line)
			if len(trimmedLine) == 0 {
				if processFrame() {
					break scanLoop
				}
				continue
			}
			if bytes.HasPrefix(trimmedLine, []byte("data:")) {
				frameData = append(frameData, bytes.Clone(bytes.TrimSpace(trimmedLine[len("data:"):])))
				continue
			}
			if bytes.HasPrefix(trimmedLine, []byte("event:")) {
				upstreamEvent = strings.TrimSpace(string(trimmedLine[len("event:"):]))
				continue
			}
			if bytes.HasPrefix(trimmedLine, []byte(":")) || bytes.HasPrefix(trimmedLine, []byte("id:")) || bytes.HasPrefix(trimmedLine, []byte("retry:")) {
				continue
			}
			if bytes.HasPrefix(trimmedLine, []byte("{")) || bytes.HasPrefix(trimmedLine, []byte("[")) {
				publishStreamError(statusErr{code: http.StatusBadGateway, msg: string(trimmedLine)}, true)
				break
			}
		}
		errScan := scanner.Err()
		if errScan == nil && !seenDone && !streamFailed && !streamAborted && len(frameData) > 0 {
			_ = processFrame()
		}
		if streamFailed || streamAborted {
			return
		}
		if errScan != nil {
			helps.RecordAPIResponseError(ctx, e.cfg, errScan)
			reporter.PublishFailure(ctx, errScan)
			select {
			case out <- cliproxyexecutor.StreamChunk{Err: errScan}:
			case <-ctx.Done():
			}
		}
		reporter.EnsurePublished(ctx)
	}()

	return &cliproxyexecutor.StreamResult{
		Headers: httpResp.Header.Clone(),
		Chunks:  out,
	}, nil
}

func (e *BedrockMantleExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	baseModel := thinking.ParseSuffix(req.Model).ModelName

	from := opts.SourceFormat
	responseFormat := cliproxyexecutor.ResponseFormatOrSource(opts)
	to := sdktranslator.FromString("openai")
	isCompat := helps.APIKeyModelIsCompat(req)
	translated := helps.TranslateRequestWithAPIKeyModelCompatibility(ctx, opts.Headers, e.cfg, from, to, baseModel, req.Payload, false, isCompat)

	translated, err := helps.ApplyRequestThinking(translated, req, opts, from.String(), to.String(), e.Identifier())
	if err != nil {
		return cliproxyexecutor.Response{}, err
	}

	enc, err := helps.TokenizerForModel(baseModel)
	if err != nil {
		return cliproxyexecutor.Response{}, fmt.Errorf("bedrock mantle executor: tokenizer init failed: %w", err)
	}

	count, err := helps.CountOpenAIChatTokens(enc, translated)
	if err != nil {
		return cliproxyexecutor.Response{}, fmt.Errorf("bedrock mantle executor: token counting failed: %w", err)
	}

	usageJSON := helps.BuildOpenAIUsageJSON(count)
	translatedUsage := sdktranslator.TranslateTokenCount(ctx, to, responseFormat, count, usageJSON)
	return cliproxyexecutor.Response{Payload: translatedUsage}, nil
}

func (e *BedrockMantleExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	return auth, nil
}
