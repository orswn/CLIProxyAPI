package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type comboExecutionContextKey struct{}

type comboRotation struct {
	mu    sync.Mutex
	state map[string]comboRotationState
}

type comboRotationState struct {
	index   int
	used    int
	members string
}

var globalComboRotation = comboRotation{state: make(map[string]comboRotationState)}

func comboExecutionContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, comboExecutionContextKey{}, true)
}

func comboExecutionActive(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	active, _ := ctx.Value(comboExecutionContextKey{}).(bool)
	return active
}

func (h *BaseAPIHandler) comboForModel(model string) (config.ComboConfig, bool) {
	if h == nil || h.Cfg == nil {
		return config.ComboConfig{}, false
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return config.ComboConfig{}, false
	}
	for _, combo := range h.Cfg.Combos {
		if combo.Disabled || !strings.EqualFold(strings.TrimSpace(combo.Name), model) {
			continue
		}
		if len(combo.Models) == 0 {
			continue
		}
		return combo, true
	}
	return config.ComboConfig{}, false
}

func comboMemberOrder(combo config.ComboConfig) []string {
	models := append([]string(nil), combo.Models...)
	if len(models) <= 1 || combo.Strategy != config.ComboStrategyRoundRobin {
		return models
	}
	key := strings.ToLower(strings.TrimSpace(combo.Name))
	globalComboRotation.mu.Lock()
	state := globalComboRotation.state[key]
	fingerprint := strings.Join(models, "\x00")
	if state.members != fingerprint {
		state = comboRotationState{members: fingerprint}
	}
	index := state.index % len(models)
	limit := combo.StickyRoundRobinLimit
	if limit < 1 {
		limit = 1
	}
	state.used++
	if state.used >= limit {
		state.index = (index + 1) % len(models)
		state.used = 0
	}
	globalComboRotation.state[key] = state
	globalComboRotation.mu.Unlock()
	return append(models[index:], models[:index]...)
}

func comboFallbackEligible(errMsg *interfaces.ErrorMessage) bool {
	if errMsg == nil {
		return false
	}
	if errors.Is(errMsg.Error, context.Canceled) || errors.Is(errMsg.Error, context.DeadlineExceeded) {
		return false
	}
	if errMsg.StatusCode == http.StatusBadRequest && errMsg.Error != nil {
		message := strings.ToLower(errMsg.Error.Error())
		return strings.Contains(message, "model_not_found") || strings.Contains(message, "unknown provider")
	}
	switch errMsg.StatusCode {
	case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden,
		http.StatusNotFound, http.StatusRequestTimeout, http.StatusTooManyRequests:
		return true
	default:
		return errMsg.StatusCode >= http.StatusInternalServerError || errMsg.StatusCode == 0
	}
}

func rewriteComboRequestModel(raw []byte, model string) []byte {
	if len(raw) == 0 || strings.TrimSpace(model) == "" || !gjson.GetBytes(raw, "model").Exists() {
		return raw
	}
	updated, err := sjson.SetBytes(raw, "model", strings.TrimSpace(model))
	if err != nil {
		return raw
	}
	return updated
}

func rewriteComboResponseModel(raw []byte, model string) []byte {
	if len(raw) == 0 || strings.TrimSpace(model) == "" {
		return raw
	}
	for _, path := range []string{"model", "modelVersion", "response.model", "response.modelVersion", "message.model"} {
		if gjson.GetBytes(raw, path).Exists() {
			if updated, err := sjson.SetBytes(raw, path, model); err == nil {
				raw = updated
			}
		}
	}
	return raw
}

func rewriteComboStreamChunk(raw []byte, model string) []byte {
	if len(raw) == 0 {
		return raw
	}
	if json.Valid(bytes.TrimSpace(raw)) {
		return rewriteComboResponseModel(raw, model)
	}
	lines := bytes.Split(raw, []byte("\n"))
	for i, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if bytes.HasPrefix(trimmed, []byte("data:")) {
			payload := bytes.TrimSpace(bytes.TrimPrefix(trimmed, []byte("data:")))
			if len(payload) > 0 && payload[0] == '{' && json.Valid(payload) {
				rewritten := rewriteComboResponseModel(payload, model)
				lines[i] = append([]byte("data: "), rewritten...)
			}
		}
	}
	return bytes.Join(lines, []byte("\n"))
}

func (h *BaseAPIHandler) executeComboStream(ctx context.Context, entryProtocol, exitProtocol string, combo config.ComboConfig, rawJSON []byte, alt string, allowImageModel bool, execOptions modelExecutionOptions) (<-chan []byte, http.Header, <-chan *interfaces.ErrorMessage) {
	if errCombo := h.validateComboExecution(ctx, combo); errCombo != nil {
		return comboErrorStream(errCombo)
	}
	if combo.Strategy == config.ComboStrategyFusion {
		return h.executeFusionStream(ctx, entryProtocol, exitProtocol, combo, rawJSON, alt, allowImageModel, execOptions)
	}
	var lastErr *interfaces.ErrorMessage
	for _, member := range comboMemberOrder(combo) {
		if errCancelled := comboContextError(ctx); errCancelled != nil {
			return comboErrorStream(errCancelled)
		}
		attemptCtx, cancel := context.WithCancel(comboExecutionContext(ctx))
		model, memberOptions := comboMemberExecution(member, execOptions)
		data, headers, errs := h.executeStreamWithAuthManagerFormats(attemptCtx, entryProtocol, exitProtocol, model, rewriteComboRequestModel(rawJSON, model), alt, allowImageModel, memberOptions)
		first, firstErr, dataOpen, errOpen := readComboStreamStart(attemptCtx, data, errs)
		if firstErr != nil {
			cancel()
			lastErr = firstErr
			if comboFallbackEligible(firstErr) {
				continue
			}
			return comboErrorStream(firstErr)
		}
		return forwardComboStream(attemptCtx, cancel, data, errs, first, dataOpen, errOpen, combo.Name, headers)
	}
	if lastErr != nil {
		return comboErrorStream(lastErr)
	}
	return comboErrorStream(&interfaces.ErrorMessage{StatusCode: http.StatusServiceUnavailable})
}

func readComboStreamStart(ctx context.Context, data <-chan []byte, errs <-chan *interfaces.ErrorMessage) ([]byte, *interfaces.ErrorMessage, bool, bool) {
	var first []byte
	for data != nil || errs != nil {
		select {
		case <-ctx.Done():
			return nil, &interfaces.ErrorMessage{StatusCode: http.StatusRequestTimeout, Error: ctx.Err()}, false, false
		case chunk, ok := <-data:
			if !ok {
				data = nil
				continue
			}
			if len(bytes.TrimSpace(chunk)) == 0 {
				continue
			}
			first = chunk
			return first, nil, true, errs != nil
		case errMsg, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if errMsg != nil {
				return nil, errMsg, data != nil, true
			}
		}
	}
	return first, &interfaces.ErrorMessage{StatusCode: http.StatusBadGateway, Error: fmt.Errorf("combo member returned an empty stream")}, false, false
}

func forwardComboStream(ctx context.Context, cancel context.CancelFunc, data <-chan []byte, errs <-chan *interfaces.ErrorMessage, first []byte, dataOpen, errOpen bool, comboName string, headers http.Header) (<-chan []byte, http.Header, <-chan *interfaces.ErrorMessage) {
	outData := make(chan []byte)
	outErr := make(chan *interfaces.ErrorMessage, 1)
	if !dataOpen {
		data = nil
	}
	if !errOpen {
		errs = nil
	}
	go func() {
		defer close(outData)
		defer close(outErr)
		defer cancel()
		sendData := func(payload []byte) bool {
			select {
			case <-ctx.Done():
				return false
			case outData <- rewriteComboStreamChunk(payload, comboName):
				return true
			}
		}
		if first != nil && !sendData(first) {
			return
		}
		for dataOpen || errOpen {
			select {
			case <-ctx.Done():
				return
			case payload, ok := <-data:
				if !ok {
					dataOpen = false
					data = nil
					continue
				}
				if !sendData(payload) {
					return
				}
			case errMsg, ok := <-errs:
				if !ok {
					errOpen = false
					errs = nil
					continue
				}
				if errMsg != nil {
					select {
					case <-ctx.Done():
					case outErr <- errMsg:
					}
					return
				}
			}
		}
	}()
	return outData, headers, outErr
}

func comboContextError(ctx context.Context) *interfaces.ErrorMessage {
	if ctx != nil && ctx.Err() != nil {
		return &interfaces.ErrorMessage{StatusCode: http.StatusRequestTimeout, Error: ctx.Err()}
	}
	return nil
}

// Explicit selections use provider::model. Bare IDs preserve legacy registry
// routing, including user-defined prefixes and model names containing slashes.
func comboMemberExecution(member string, options modelExecutionOptions) (string, modelExecutionOptions) {
	if provider, model, ok := strings.Cut(member, "::"); ok && provider != "" && model != "" {
		options.ForcedProvider = strings.ToLower(strings.TrimSpace(provider))
		options.AuthSelectionModel = strings.TrimSpace(model)
		return strings.TrimSpace(model), options
	}
	return member, options
}

func (h *BaseAPIHandler) validateComboExecution(ctx context.Context, combo config.ComboConfig) *interfaces.ErrorMessage {
	if errCancelled := comboContextError(ctx); errCancelled != nil {
		return errCancelled
	}
	if errValidate := config.ValidateCombos(h.Cfg.Combos); errValidate != nil {
		return &interfaces.ErrorMessage{StatusCode: http.StatusBadRequest, Error: errValidate}
	}
	return nil
}

func comboErrorStream(errMsg *interfaces.ErrorMessage) (<-chan []byte, http.Header, <-chan *interfaces.ErrorMessage) {
	data := make(chan []byte)
	errs := make(chan *interfaces.ErrorMessage, 1)
	errs <- errMsg
	close(errs)
	close(data)
	return data, nil, errs
}

func intersectEffortTiers(tiersA, tiersB []string) []string {
	if len(tiersA) == 0 || len(tiersB) == 0 {
		return nil
	}
	setB := make(map[string]struct{}, len(tiersB))
	for _, t := range tiersB {
		setB[strings.ToLower(strings.TrimSpace(t))] = struct{}{}
	}
	var out []string
	for _, t := range tiersA {
		norm := strings.ToLower(strings.TrimSpace(t))
		if _, ok := setB[norm]; ok {
			out = append(out, norm)
		}
	}
	return out
}

// ComboCatalogModels returns protocol-shaped model metadata for enabled combos.
func (h *BaseAPIHandler) ComboCatalogModels(format string) []map[string]any {
	if h == nil || h.Cfg == nil || len(h.Cfg.Combos) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(h.Cfg.Combos))
	reg := registry.GetGlobalRegistry()
	for _, combo := range h.Cfg.Combos {
		name := strings.TrimSpace(combo.Name)
		if combo.Disabled || name == "" || len(combo.Models) == 0 {
			continue
		}

		activeMembers := 0
		for _, member := range combo.Models {
			if reg.GetModelCount(member) > 0 {
				activeMembers++
			}
		}

		if combo.Strategy == "fusion" {
			minRequired := combo.MinSuccessfulModels
			if minRequired <= 0 {
				minRequired = 1
			}
			if activeMembers < minRequired {
				continue
			}
			if combo.JudgeModel != "" && reg.GetModelCount(combo.JudgeModel) == 0 {
				continue
			}
		} else {
			if activeMembers == 0 {
				continue
			}
		}

		displayName := strings.TrimSpace(combo.DisplayName)
		if displayName == "" {
			displayName = name
		}
		if format == "gemini" {
			out = append(out, map[string]any{
				"name":                       name,
				"displayName":                displayName,
				"description":                "CLIProxyAPI model combo",
				"supportedGenerationMethods": []string{"generateContent"},
			})
			continue
		}

		var minContext int
		var minOutput int
		allReasoning := true
		allVision := true
		var commonTiers []string
		firstModel := true

		for _, member := range combo.Models {
			info := reg.GetModelInfo(member, "")
			if info == nil {
				info = registry.LookupStaticModelInfo(member)
			}
			if info == nil {
				continue
			}
			if info.ContextLength > 0 && (minContext == 0 || info.ContextLength < minContext) {
				minContext = info.ContextLength
			}
			if info.MaxCompletionTokens > 0 && (minOutput == 0 || info.MaxCompletionTokens < minOutput) {
				minOutput = info.MaxCompletionTokens
			}
			if info.Thinking == nil {
				allReasoning = false
			} else {
				if firstModel {
					commonTiers = append([]string(nil), info.Thinking.Levels...)
				} else {
					commonTiers = intersectEffortTiers(commonTiers, info.Thinking.Levels)
				}
			}
			hasImage := false
			for _, mod := range info.SupportedInputModalities {
				if strings.EqualFold(mod, "image") {
					hasImage = true
					break
				}
			}
			if !hasImage {
				allVision = false
			}
			firstModel = false
		}

		entry := map[string]any{
			"id":           name,
			"object":       "model",
			"created":      int64(0),
			"owned_by":     "combo",
			"display_name": displayName,
		}
		if minContext > 0 {
			entry["context_length"] = minContext
		}
		if minOutput > 0 {
			entry["max_completion_tokens"] = minOutput
		}
		if allReasoning {
			entry["reasoning"] = true
		}
		if allVision {
			entry["input"] = []string{"text", "image"}
		} else {
			entry["input"] = []string{"text"}
		}
		capabilities := map[string]any{}
		if minContext > 0 {
			capabilities["contextWindow"] = minContext
		}
		if minOutput > 0 {
			capabilities["maxOutput"] = minOutput
		}
		if allReasoning {
			capabilities["reasoning"] = true
			if len(commonTiers) > 0 {
				capabilities["effort_tiers"] = commonTiers
			}
		}
		if allVision {
			capabilities["vision"] = true
		}
		if len(capabilities) > 0 {
			entry["capabilities"] = capabilities
		}

		out = append(out, entry)
	}
	return out
}
