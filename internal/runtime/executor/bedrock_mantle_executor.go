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
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/bedrockmantle"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/sjson"
)

// BedrockMantleExecutor implements execution against AWS Bedrock Mantle
// OpenAI-compatible endpoints signed with AWS SigV4.
type BedrockMantleExecutor struct {
	cfg       *config.Config
	credCache *bedrockmantle.ProviderCache
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
		cfg:       cfg,
		credCache: bedrockmantle.NewProviderCache(nil),
	}
}

// Identifier implements cliproxyauth.ProviderExecutor.
func (e *BedrockMantleExecutor) Identifier() string { return "bedrock-mantle" }

// RequestToFormat reports the upstream request format used after auth selection.
func (e *BedrockMantleExecutor) RequestToFormat(_ cliproxyexecutor.Request, opts cliproxyexecutor.Options) sdktranslator.Format {
	if opts.SourceFormat == sdktranslator.FormatOpenAIResponse || opts.SourceFormat == sdktranslator.FormatClaude {
		return sdktranslator.FormatOpenAIResponse
	}
	return sdktranslator.FormatOpenAI
}

func bedrockMantleEndpointURL(region string, format sdktranslator.Format) string {
	path := "chat/completions"
	if format == sdktranslator.FormatOpenAIResponse {
		path = "responses"
	}
	return fmt.Sprintf("https://bedrock-mantle.%s.api.aws/openai/v1/%s", region, path)
}

func isBedrockMantleResponsesTerminalEvent(eventName string) bool {
	switch eventName {
	case "response.completed", "response.incomplete", "response.failed":
		return true
	default:
		return false
	}
}

// storageForAuth builds the credential storage for an auth from its metadata
// (auth-dir files) or attributes (config-synthesized entries).
func (e *BedrockMantleExecutor) storageForAuth(auth *cliproxyauth.Auth) *bedrockmantle.Storage {
	if auth == nil {
		return nil
	}
	if s := bedrockmantle.FromMetadata(auth.Metadata); s != nil && bedrockmantle.DetectMode(s) != "" {
		return s
	}
	if auth.Attributes != nil {
		s := &bedrockmantle.Storage{
			Profile:         strings.TrimSpace(auth.Attributes["profile"]),
			AWSDir:          strings.TrimSpace(auth.Attributes["aws_dir"]),
			AccessKeyID:     strings.TrimSpace(auth.Attributes["access_key_id"]),
			SecretAccessKey: strings.TrimSpace(auth.Attributes["secret_access_key"]),
			SessionToken:    strings.TrimSpace(auth.Attributes["session_token"]),
			RoleARN:         strings.TrimSpace(auth.Attributes["role_arn"]),
			DefaultRegion:   strings.TrimSpace(auth.Attributes["default_region"]),
		}
		if cfgEntry := e.resolveMantleConfig(auth); cfgEntry != nil {
			s.ModelRegions = cfgEntry.ModelRegions
			if s.DefaultRegion == "" {
				s.DefaultRegion = cfgEntry.DefaultRegion
			}
		}
		if mode := bedrockmantle.DetectMode(s); mode != "" {
			s.AuthMode = mode
			return s
		}
	}
	return nil
}

func (e *BedrockMantleExecutor) credentialCacheKey(auth *cliproxyauth.Auth, s *bedrockmantle.Storage) string {
	if auth != nil && strings.TrimSpace(auth.ID) != "" {
		if s != nil && s.AuthMode == bedrockmantle.ModeSSO {
			// Token rotation must rebuild the SSO provider.
			return auth.ID + "|" + s.LastRefresh
		}
		return auth.ID
	}
	if s != nil {
		return s.AuthMode + "|" + s.Profile + "|" + s.AccessKeyID + "|" + s.AccountID + "|" + s.RoleName
	}
	return "__default__"
}

func (e *BedrockMantleExecutor) resolveCredentials(ctx context.Context, auth *cliproxyauth.Auth) (util.AwsCredentials, error) {
	s := e.storageForAuth(auth)
	if s == nil {
		return util.AwsCredentials{}, fmt.Errorf("bedrock mantle: credential has no usable auth mode")
	}
	if s.AuthMode == bedrockmantle.ModeStatic && strings.TrimSpace(s.RoleARN) == "" {
		return util.AwsCredentials{
			AccessKeyID:     s.AccessKeyID,
			SecretAccessKey: s.SecretAccessKey,
			SessionToken:    s.SessionToken,
		}, nil
	}
	key := e.credentialCacheKey(auth, s)
	creds, err := e.credCache.Retrieve(ctx, key, s)
	if err != nil {
		e.credCache.Invalidate(key)
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
	if auth == nil || auth.AuthSourceKind() != cliproxyauth.AuthSourceConfig || auth.Attributes == nil {
		return nil
	}
	idx, err := strconv.Atoi(strings.TrimSpace(auth.Attributes[cliproxyauth.AttributeConfigIndex]))
	if err != nil || idx < 0 || idx >= len(e.cfg.BedrockMantle) {
		return nil
	}
	return &e.cfg.BedrockMantle[idx]
}

func (e *BedrockMantleExecutor) resolveRegion(auth *cliproxyauth.Auth, modelName string) string {
	if s := e.storageForAuth(auth); s != nil {
		return s.ResolveRegion(modelName)
	}
	if mantleConfig := e.resolveMantleConfig(auth); mantleConfig != nil {
		return mantleConfig.ResolveRegion(modelName)
	}
	return bedrockmantle.DefaultBedrockRegion
}

// prepareResponsesPayload applies the shared Responses input hygiene and drops
// conversation state Mantle cannot resolve. Bedrock does not persist responses,
// so a previous_response_id carried over from another provider only produces an
// upstream error.
func (e *BedrockMantleExecutor) prepareResponsesPayload(ctx context.Context, payload []byte, to sdktranslator.Format) []byte {
	if to != sdktranslator.FormatOpenAIResponse {
		return payload
	}
	payload = prepareOpenAIResponsesInput(ctx, "bedrock mantle", payload)
	if updated, errDelete := sjson.DeleteBytes(payload, "previous_response_id"); errDelete == nil {
		payload = updated
	}
	return payload
}

func (e *BedrockMantleExecutor) sanitizeMantlePayload(payload []byte, modelName string) []byte {
	m := strings.ToLower(modelName)
	if strings.Contains(m, "gpt-6") || strings.Contains(m, "gpt-5.6") || strings.Contains(m, "astra") {
		payload, _ = sjson.DeleteBytes(payload, "max_tokens")
		payload, _ = sjson.DeleteBytes(payload, "max_output_tokens")
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
	from := opts.SourceFormat
	responseFormat := cliproxyexecutor.ResponseFormatOrSource(opts)
	to := e.RequestToFormat(req, opts)
	endpointURL := bedrockMantleEndpointURL(region, to)

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
	translated = e.prepareResponsesPayload(ctx, translated, to)
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
	from := opts.SourceFormat
	responseFormat := cliproxyexecutor.ResponseFormatOrSource(opts)
	to := e.RequestToFormat(req, opts)
	endpointURL := bedrockMantleEndpointURL(region, to)

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
	translated = e.prepareResponsesPayload(ctx, translated, to)

	if to == sdktranslator.FormatOpenAI {
		translated = helps.SetBoolIfDifferent(translated, "stream_options.include_usage", true)
	}
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
				if to == sdktranslator.FormatOpenAIResponse {
					if usage, ok := helps.ParseCodexUsage(dataPayload); ok {
						streamUsage.Observe(usage, true)
					}
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
			if isDone || (to == sdktranslator.FormatOpenAIResponse && isBedrockMantleResponsesTerminalEvent(eventName)) {
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

// Refresh rotates the IAM Identity Center access token for SSO credentials.
// Static and profile credentials have nothing to refresh.
func (e *BedrockMantleExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	if auth == nil {
		return nil, fmt.Errorf("bedrock mantle executor: auth is nil")
	}
	s := bedrockmantle.FromMetadata(auth.Metadata)
	if s == nil || s.AuthMode != bedrockmantle.ModeSSO {
		return auth, nil
	}
	sess := s.SSOSession()
	if strings.TrimSpace(sess.Token.RefreshToken) == "" {
		return nil, statusErr{code: http.StatusUnauthorized, msg: "bedrock mantle sso: refresh token missing; re-login required"}
	}
	client := bedrockmantle.NewClient(s.SSORegion, nil)
	tok, err := client.RefreshToken(ctx, sess)
	if err != nil {
		return nil, statusErr{code: http.StatusUnauthorized, msg: err.Error()}
	}
	s.ApplyToken(tok)
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	auth.Metadata[bedrockmantle.KeyAccessToken] = s.AccessToken
	auth.Metadata[bedrockmantle.KeyRefreshToken] = s.RefreshToken
	auth.Metadata[bedrockmantle.KeyExpired] = s.Expired
	auth.Metadata[bedrockmantle.KeyLastRefresh] = s.LastRefresh
	e.credCache.Invalidate(e.credentialCacheKey(auth, s))
	return auth, nil
}
