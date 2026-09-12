// Package registry provides model definitions and lookup helpers for various AI providers.
// Static model metadata is loaded from the embedded models.json file and can be refreshed from network.
package registry

import (
	"strings"
)

const (
	codexBuiltinImage15ModelID         = "gpt-image-1.5"
	codexBuiltinImageModelID           = "gpt-image-2"
	codexBuiltinImage25FlareModelID    = "gpt-image-2.5-flare"
	codexBuiltinImage25SunburstModelID = "gpt-image-2.5-sunburst"
	codexBuiltinImage25ModelID         = "gpt-image-2.5"
	xaiBuiltinImageModelID             = "grok-imagine-image"
	xaiBuiltinImageQualityModelID      = "grok-imagine-image-quality"
	xaiBuiltinImage20ModelID           = "grok-imagine-image-2.0"
	xaiBuiltinVideoModelID             = "grok-imagine-video"
	xaiBuiltinVideo15ModelID           = "grok-imagine-video-1.5"
	xaiBuiltinVideo15PreviewID         = "grok-imagine-video-1.5-preview"
)

// staticModelsJSON mirrors the top-level structure of models.json.
type staticModelsJSON struct {
	Claude      []*ModelInfo `json:"claude"`
	Gemini      []*ModelInfo `json:"gemini"`
	Vertex      []*ModelInfo `json:"vertex"`
	AIStudio    []*ModelInfo `json:"aistudio"`
	CodexFree   []*ModelInfo `json:"codex-free"`
	CodexTeam   []*ModelInfo `json:"codex-team"`
	CodexPlus   []*ModelInfo `json:"codex-plus"`
	CodexPro    []*ModelInfo `json:"codex-pro"`
	Kimi        []*ModelInfo `json:"kimi"`
	Antigravity []*ModelInfo `json:"antigravity"`
	XAI         []*ModelInfo `json:"xai"`
}

// GetClaudeModels returns the standard Claude model definitions.
func GetClaudeModels() []*ModelInfo {
	return cloneModelInfos(getModels().Claude)
}

// GetGeminiModels returns the standard Gemini model definitions.
func GetGeminiModels() []*ModelInfo {
	return cloneModelInfos(getModels().Gemini)
}

// GetGeminiVertexModels returns Gemini model definitions for Vertex AI.
func GetGeminiVertexModels() []*ModelInfo {
	return cloneModelInfos(getModels().Vertex)
}

// GetAIStudioModels returns model definitions for AI Studio.
func GetAIStudioModels() []*ModelInfo {
	return cloneModelInfos(getModels().AIStudio)
}

// GetCodexFreeModels returns model definitions for the Codex free plan tier.
func GetCodexFreeModels() []*ModelInfo {
	return WithCodexBuiltins(cloneModelInfos(getModels().CodexFree))
}

// GetCodexTeamModels returns model definitions for the Codex team plan tier.
func GetCodexTeamModels() []*ModelInfo {
	return WithCodexBuiltins(cloneModelInfos(getModels().CodexTeam))
}

// GetCodexPlusModels returns model definitions for the Codex plus plan tier.
func GetCodexPlusModels() []*ModelInfo {
	return WithCodexBuiltins(cloneModelInfos(getModels().CodexPlus))
}

// GetCodexProModels returns model definitions for the Codex pro plan tier.
func GetCodexProModels() []*ModelInfo {
	return WithCodexBuiltins(cloneModelInfos(getModels().CodexPro))
}

// GetKimiModels returns the standard Kimi (Moonshot AI) model definitions.
func GetKimiModels() []*ModelInfo {
	return cloneModelInfos(getModels().Kimi)
}

// GetAntigravityModels returns the standard Antigravity model definitions.
func GetAntigravityModels() []*ModelInfo {
	return cloneModelInfos(getModels().Antigravity)
}

// AntigravityWebSearchModelFor returns the Antigravity model that should run a
// native web search request for modelID.
func AntigravityWebSearchModelFor(modelID string) string {
	modelID = normalizeAntigravityCapabilityModelID(modelID)
	if modelID == "" {
		return ""
	}
	for _, model := range GetGlobalRegistry().GetAvailableModelsByProvider("antigravity") {
		if model == nil {
			continue
		}
		currentModelID := normalizeAntigravityCapabilityModelID(model.ID)
		if currentModelID == "" {
			continue
		}
		if currentModelID == modelID {
			if model.SupportsWebSearch {
				return currentModelID
			}
			return ""
		}
	}
	return ""
}

// GetXAIModels returns the standard xAI Grok model definitions.
func GetXAIModels() []*ModelInfo {
	return WithXAIBuiltins(cloneModelInfos(getModels().XAI))
}

// WithCodexBuiltins injects hard-coded Codex-only model definitions that should
// not depend on remote models.json updates. Built-ins replace any matching IDs
// already present in the provided slice.
func WithCodexBuiltins(models []*ModelInfo) []*ModelInfo {
	return upsertModelInfos(models,
		codexBuiltinImage15ModelInfo(),
		codexBuiltinImageModelInfo(),
		codexBuiltinImage25FlareModelInfo(),
		codexBuiltinImage25SunburstModelInfo(),
		codexBuiltinImage25ModelInfo(),
	)
}

// WithXAIBuiltins injects hard-coded xAI image/video model definitions that should
// not depend on remote models.json updates.
func WithXAIBuiltins(models []*ModelInfo) []*ModelInfo {
	return upsertModelInfos(models, xaiBuiltinImageModelInfo(), xaiBuiltinImageQualityModelInfo(), xaiBuiltinImage20ModelInfo(), xaiBuiltinVideoModelInfo(), xaiBuiltinVideo15ModelInfo(), xaiBuiltinVideo15PreviewModelInfo())
}

func normalizeAntigravityCapabilityModelID(modelID string) string {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	if open := strings.LastIndex(modelID, "("); open >= 0 && strings.HasSuffix(modelID, ")") {
		modelID = strings.TrimSpace(modelID[:open])
	}
	return modelID
}

func codexBuiltinImage15ModelInfo() *ModelInfo {
	return &ModelInfo{
		ID:          codexBuiltinImage15ModelID,
		Object:      "model",
		Created:     1704067200, // 2024-01-01
		OwnedBy:     "openai",
		Type:        "openai",
		DisplayName: "GPT Image 1.5",
		Version:     codexBuiltinImage15ModelID,
	}
}

func codexBuiltinImageModelInfo() *ModelInfo {
	return &ModelInfo{
		ID:          codexBuiltinImageModelID,
		Object:      "model",
		Created:     1704067200, // 2024-01-01
		OwnedBy:     "openai",
		Type:        "openai",
		DisplayName: "GPT Image 2",
		Version:     codexBuiltinImageModelID,
	}
}

func codexBuiltinImage25FlareModelInfo() *ModelInfo {
	return &ModelInfo{
		ID:          codexBuiltinImage25FlareModelID,
		Object:      "model",
		Created:     1704067200, // 2024-01-01
		OwnedBy:     "openai",
		Type:        "openai",
		DisplayName: "GPT Image 2.5 Flare",
		Version:     codexBuiltinImage25FlareModelID,
	}
}

func codexBuiltinImage25SunburstModelInfo() *ModelInfo {
	return &ModelInfo{
		ID:          codexBuiltinImage25SunburstModelID,
		Object:      "model",
		Created:     1704067200, // 2024-01-01
		OwnedBy:     "openai",
		Type:        "openai",
		DisplayName: "GPT Image 2.5 Sunburst",
		Version:     codexBuiltinImage25SunburstModelID,
	}
}

func codexBuiltinImage25ModelInfo() *ModelInfo {
	return &ModelInfo{
		ID:          codexBuiltinImage25ModelID,
		Object:      "model",
		Created:     1704067200, // 2024-01-01
		OwnedBy:     "openai",
		Type:        "openai",
		DisplayName: "GPT Image 2.5",
		Version:     codexBuiltinImage25ModelID,
	}
}

func xaiBuiltinImageModelInfo() *ModelInfo {
	return &ModelInfo{
		ID:          xaiBuiltinImageModelID,
		Object:      "model",
		Created:     1735689600, // 2025-01-01
		OwnedBy:     "xai",
		Type:        "xai",
		DisplayName: "Grok Imagine Image",
		Name:        xaiBuiltinImageModelID,
		Description: "xAI Grok image generation model.",
	}
}

func xaiBuiltinImageQualityModelInfo() *ModelInfo {
	return &ModelInfo{
		ID:          xaiBuiltinImageQualityModelID,
		Object:      "model",
		Created:     1735689600, // 2025-01-01
		OwnedBy:     "xai",
		Type:        "xai",
		DisplayName: "Grok Imagine Image Quality",
		Name:        xaiBuiltinImageQualityModelID,
		Description: "xAI Grok higher-fidelity image generation model.",
	}
}

func xaiBuiltinImage20ModelInfo() *ModelInfo {
	return &ModelInfo{
		ID:          xaiBuiltinImage20ModelID,
		Object:      "model",
		Created:     1786060800, // 2026-08-07
		OwnedBy:     "xai",
		Type:        "xai",
		DisplayName: "Grok Imagine Image 2.0",
		Name:        xaiBuiltinImage20ModelID,
		Description: "xAI Grok image generation model.",
	}
}

func xaiBuiltinVideoModelInfo() *ModelInfo {
	return &ModelInfo{
		ID:          xaiBuiltinVideoModelID,
		Object:      "model",
		Created:     1735689600, // 2025-01-01
		OwnedBy:     "xai",
		Type:        "xai",
		DisplayName: "Grok Imagine Video",
		Name:        xaiBuiltinVideoModelID,
		Description: "xAI Grok video generation model.",
	}
}

func xaiBuiltinVideo15ModelInfo() *ModelInfo {
	return &ModelInfo{
		ID:          xaiBuiltinVideo15ModelID,
		Object:      "model",
		Created:     1735689600, // 2025-01-01
		OwnedBy:     "xai",
		Type:        "xai",
		DisplayName: "Grok Imagine Video 1.5",
		Name:        xaiBuiltinVideo15ModelID,
		Description: "xAI Grok video generation model.",
	}
}

func xaiBuiltinVideo15PreviewModelInfo() *ModelInfo {
	return &ModelInfo{
		ID:          xaiBuiltinVideo15PreviewID,
		Object:      "model",
		Created:     1735689600, // 2025-01-01
		OwnedBy:     "xai",
		Type:        "xai",
		DisplayName: "Grok Imagine Video 1.5 Preview",
		Name:        xaiBuiltinVideo15PreviewID,
		Description: "Compatibility alias for the xAI Grok video generation model.",
	}
}

// GetBedrockMantleModels returns standard OpenAI model definitions served on Bedrock Mantle.
func GetBedrockMantleModels() []*ModelInfo {
	return []*ModelInfo{
		bedrockMantleLunaModelInfo("openai.gpt-5.6-luna"),
		bedrockMantleLunaModelInfo("gpt-5.6-luna"),
		bedrockMantleTerraModelInfo("openai.gpt-5.6-terra"),
		bedrockMantleTerraModelInfo("gpt-5.6-terra"),
		bedrockMantleSolModelInfo("openai.gpt-5.6-sol"),
		bedrockMantleSolModelInfo("gpt-5.6-sol"),
		bedrockMantleAstraModelInfo("openai.gpt-6-astra"),
		bedrockMantleAstraModelInfo("gpt-6-astra"),
		bedrockMantleGPT55ModelInfo("openai.gpt-5.5"),
		bedrockMantleGPT55ModelInfo("gpt-5.5"),
		bedrockMantleGPT54ModelInfo("openai.gpt-5.4"),
		bedrockMantleGPT54ModelInfo("gpt-5.4"),
		bedrockMantleCyberModelInfo("openai.gpt-5.6-cyber"),
		bedrockMantleCyberModelInfo("gpt-5.6-cyber"),
		bedrockMantleDaybreakBlueSolModelInfo("openai.gpt-daybreak-blue-5.6-sol"),
		bedrockMantleDaybreakBlueSolModelInfo("gpt-daybreak-blue-5.6-sol"),
		bedrockMantleOSS120BModelInfo("openai.gpt-oss-120b"),
		bedrockMantleOSS120BModelInfo("gpt-oss-120b"),
		bedrockMantleOSS20BModelInfo("openai.gpt-oss-20b"),
		bedrockMantleOSS20BModelInfo("gpt-oss-20b"),
		bedrockMantleOSSSafeguard120BModelInfo("openai.gpt-oss-safeguard-120b"),
		bedrockMantleOSSSafeguard120BModelInfo("gpt-oss-safeguard-120b"),
		bedrockMantleOSSSafeguard20BModelInfo("openai.gpt-oss-safeguard-20b"),
		bedrockMantleOSSSafeguard20BModelInfo("gpt-oss-safeguard-20b"),
	}
}

func bedrockMantleLunaModelInfo(id string) *ModelInfo {
	return &ModelInfo{
		ID:                        id,
		Object:                    "model",
		Created:                   1783616400,
		OwnedBy:                   "bedrock-mantle",
		Type:                      "openai",
		DisplayName:               "GPT 5.6 Luna",
		Version:                   "gpt-5.6",
		Description:               "Fast and affordable agentic coding model on Bedrock Mantle.",
		ContextLength:             1000000,
		MaxCompletionTokens:       128000,
		SupportedParameters:       []string{"tools"},
		SupportedInputModalities:  []string{"text", "image"},
		SupportedOutputModalities: []string{"text"},
		Thinking:                  &ThinkingSupport{Levels: []string{"none", "low", "medium", "high", "xhigh", "max"}},
		Pricing: &ModelPricing{
			Input:         0.22,
			Output:        1.32,
			Cached:        0.022,
			CacheCreation: 0.275,
			Reasoning:     1.32,
			LongContext: &LongContextPricing{
				Threshold:     272000,
				Input:         0.44,
				Output:        1.98,
				Cached:        0.044,
				CacheCreation: 0.55,
				Reasoning:     1.98,
			},
		},
	}
}

func bedrockMantleTerraModelInfo(id string) *ModelInfo {
	return &ModelInfo{
		ID:                        id,
		Object:                    "model",
		Created:                   1783616400,
		OwnedBy:                   "bedrock-mantle",
		Type:                      "openai",
		DisplayName:               "GPT 5.6 Terra",
		Version:                   "gpt-5.6",
		Description:               "Balanced agentic coding model for everyday work on Bedrock Mantle.",
		ContextLength:             1000000,
		MaxCompletionTokens:       128000,
		SupportedParameters:       []string{"tools"},
		SupportedInputModalities:  []string{"text", "image"},
		SupportedOutputModalities: []string{"text"},
		Thinking:                  &ThinkingSupport{Levels: []string{"none", "low", "medium", "high", "xhigh", "max"}},
		Pricing: &ModelPricing{
			Input:         2.20,
			Output:        13.20,
			Cached:        0.22,
			CacheCreation: 2.75,
			Reasoning:     13.20,
			LongContext: &LongContextPricing{
				Threshold:     272000,
				Input:         4.40,
				Output:        19.80,
				Cached:        0.44,
				CacheCreation: 5.50,
				Reasoning:     19.80,
			},
		},
	}
}

func bedrockMantleSolModelInfo(id string) *ModelInfo {
	return &ModelInfo{
		ID:                        id,
		Object:                    "model",
		Created:                   1783616400,
		OwnedBy:                   "bedrock-mantle",
		Type:                      "openai",
		DisplayName:               "GPT 5.6 Sol",
		Version:                   "gpt-5.6",
		Description:               "Frontier reasoning and agentic coding model on Bedrock Mantle.",
		ContextLength:             1000000,
		MaxCompletionTokens:       128000,
		SupportedParameters:       []string{"tools"},
		SupportedInputModalities:  []string{"text", "image"},
		SupportedOutputModalities: []string{"text"},
		Thinking:                  &ThinkingSupport{Levels: []string{"none", "low", "medium", "high", "xhigh", "max"}},
		Pricing: &ModelPricing{
			Input:         4.40,
			Output:        22.00,
			Cached:        0.44,
			CacheCreation: 5.50,
			Reasoning:     22.00,
			LongContext: &LongContextPricing{
				Threshold:     272000,
				Input:         8.80,
				Output:        33.00,
				Cached:        0.88,
				CacheCreation: 11.00,
				Reasoning:     33.00,
			},
		},
	}
}

func bedrockMantleAstraModelInfo(id string) *ModelInfo {
	return &ModelInfo{
		ID:                        id,
		Object:                    "model",
		Created:                   1788868800,
		OwnedBy:                   "bedrock-mantle",
		Type:                      "openai",
		DisplayName:               "GPT 6.0 Astra",
		Version:                   "gpt-6",
		Description:               "Frontier reasoning and coding model on Bedrock Mantle.",
		ContextLength:             1050000,
		MaxCompletionTokens:       128000,
		SupportedParameters:       []string{"tools"},
		SupportedInputModalities:  []string{"text", "image"},
		SupportedOutputModalities: []string{"text"},
		Thinking:                  &ThinkingSupport{Levels: []string{"none", "low", "medium", "high", "xhigh", "ultra"}},
		Pricing: &ModelPricing{
			Input:         10.00,
			Output:        50.00,
			Cached:        1.00,
			CacheCreation: 12.50,
			Reasoning:     50.00,
			LongContext: &LongContextPricing{
				Threshold:     272000,
				Input:         20.00,
				Output:        75.00,
				Cached:        2.00,
				CacheCreation: 25.00,
				Reasoning:     75.00,
			},
		},
	}
}

func bedrockMantleGPT55ModelInfo(id string) *ModelInfo {
	return &ModelInfo{
		ID:                        id,
		Object:                    "model",
		Created:                   1780300800,
		OwnedBy:                   "bedrock-mantle",
		Type:                      "openai",
		DisplayName:               "GPT 5.5",
		Version:                   "gpt-5.5",
		Description:               "Frontier reasoning and professional workflow model on Bedrock Mantle.",
		ContextLength:             272000,
		MaxCompletionTokens:       128000,
		SupportedParameters:       []string{"tools"},
		SupportedInputModalities:  []string{"text", "image"},
		SupportedOutputModalities: []string{"text"},
		Thinking:                  &ThinkingSupport{Levels: []string{"none", "low", "medium", "high", "xhigh"}},
		Pricing: &ModelPricing{
			Input:         5.50,
			Output:        33.00,
			Cached:        0.55,
			CacheCreation: 5.50,
			Reasoning:     33.00,
		},
	}
}

func bedrockMantleGPT54ModelInfo(id string) *ModelInfo {
	return &ModelInfo{
		ID:                        id,
		Object:                    "model",
		Created:                   1780300800,
		OwnedBy:                   "bedrock-mantle",
		Type:                      "openai",
		DisplayName:               "GPT 5.4",
		Version:                   "gpt-5.4",
		Description:               "Frontier reasoning and tool-use model on Bedrock Mantle.",
		ContextLength:             272000,
		MaxCompletionTokens:       128000,
		SupportedParameters:       []string{"tools"},
		SupportedInputModalities:  []string{"text", "image"},
		SupportedOutputModalities: []string{"text"},
		Thinking:                  &ThinkingSupport{Levels: []string{"none", "low", "medium", "high"}},
		Pricing: &ModelPricing{
			Input:         2.75,
			Output:        16.50,
			Cached:        0.275,
			CacheCreation: 2.75,
			Reasoning:     16.50,
		},
	}
}

func bedrockMantleCyberModelInfo(id string) *ModelInfo {
	return &ModelInfo{
		ID:                        id,
		Object:                    "model",
		Created:                   1783616400,
		OwnedBy:                   "bedrock-mantle",
		Type:                      "openai",
		DisplayName:               "Daybreak Red: GPT 5.6 Cyber",
		Version:                   "gpt-5.6",
		Description:               "Advanced cybersecurity model on Bedrock Mantle.",
		ContextLength:             1000000,
		MaxCompletionTokens:       128000,
		SupportedParameters:       []string{"tools"},
		SupportedInputModalities:  []string{"text", "image"},
		SupportedOutputModalities: []string{"text"},
		Thinking:                  &ThinkingSupport{Levels: []string{"none", "low", "medium", "high", "xhigh", "max"}},
		Pricing: &ModelPricing{
			Input:         13.75,
			Output:        82.50,
			Cached:        1.375,
			CacheCreation: 17.1875,
			Reasoning:     82.50,
		},
	}
}

func bedrockMantleDaybreakBlueSolModelInfo(id string) *ModelInfo {
	return &ModelInfo{
		ID:                        id,
		Object:                    "model",
		Created:                   1783616400,
		OwnedBy:                   "bedrock-mantle",
		Type:                      "openai",
		DisplayName:               "Daybreak Blue: GPT 5.6 Sol",
		Version:                   "gpt-5.6",
		Description:               "Authorized defender cybersecurity model on Bedrock Mantle.",
		ContextLength:             1000000,
		MaxCompletionTokens:       128000,
		SupportedParameters:       []string{"tools"},
		SupportedInputModalities:  []string{"text", "image"},
		SupportedOutputModalities: []string{"text"},
		Thinking:                  &ThinkingSupport{Levels: []string{"none", "low", "medium", "high", "xhigh", "max"}},
		Pricing: &ModelPricing{
			Input:         5.50,
			Output:        33.00,
			Cached:        0.55,
			CacheCreation: 6.875,
			Reasoning:     33.00,
			LongContext: &LongContextPricing{
				Threshold:     272000,
				Input:         11.00,
				Output:        49.50,
				Cached:        1.10,
				CacheCreation: 13.75,
				Reasoning:     49.50,
			},
		},
	}
}

func bedrockMantleOSS120BModelInfo(id string) *ModelInfo {
	return &ModelInfo{
		ID:                        id,
		Object:                    "model",
		Created:                   1754352000,
		OwnedBy:                   "bedrock-mantle",
		Type:                      "openai",
		DisplayName:               "GPT OSS 120B",
		Version:                   "gpt-oss",
		Description:               "Open-source general-purpose 120B parameter model on Bedrock Mantle.",
		ContextLength:             128000,
		MaxCompletionTokens:       16000,
		SupportedParameters:       []string{"tools"},
		SupportedInputModalities:  []string{"text"},
		SupportedOutputModalities: []string{"text"},
	}
}

func bedrockMantleOSS20BModelInfo(id string) *ModelInfo {
	return &ModelInfo{
		ID:                        id,
		Object:                    "model",
		Created:                   1754352000,
		OwnedBy:                   "bedrock-mantle",
		Type:                      "openai",
		DisplayName:               "GPT OSS 20B",
		Version:                   "gpt-oss",
		Description:               "Open-source compact 20B parameter model on Bedrock Mantle.",
		ContextLength:             128000,
		MaxCompletionTokens:       16000,
		SupportedParameters:       []string{"tools"},
		SupportedInputModalities:  []string{"text"},
		SupportedOutputModalities: []string{"text"},
	}
}

func bedrockMantleOSSSafeguard120BModelInfo(id string) *ModelInfo {
	return &ModelInfo{
		ID:                        id,
		Object:                    "model",
		Created:                   1754352000,
		OwnedBy:                   "bedrock-mantle",
		Type:                      "openai",
		DisplayName:               "GPT OSS Safeguard 120B",
		Version:                   "gpt-oss",
		Description:               "Open-source safety model for content moderation on Bedrock Mantle.",
		ContextLength:             128000,
		MaxCompletionTokens:       16000,
		SupportedInputModalities:  []string{"text"},
		SupportedOutputModalities: []string{"text"},
	}
}

func bedrockMantleOSSSafeguard20BModelInfo(id string) *ModelInfo {
	return &ModelInfo{
		ID:                        id,
		Object:                    "model",
		Created:                   1754352000,
		OwnedBy:                   "bedrock-mantle",
		Type:                      "openai",
		DisplayName:               "GPT OSS Safeguard 20B",
		Version:                   "gpt-oss",
		Description:               "Open-source compact safety model for moderation on Bedrock Mantle.",
		ContextLength:             128000,
		MaxCompletionTokens:       16000,
		SupportedInputModalities:  []string{"text"},
		SupportedOutputModalities: []string{"text"},
	}
}

// LookupStaticBedrockMantleModelInfo searches static Bedrock Mantle model definitions by ID.
func LookupStaticBedrockMantleModelInfo(modelID string) *ModelInfo {
	trimmed := strings.ToLower(strings.TrimSpace(modelID))
	if trimmed == "" {
		return nil
	}
	stripped := strings.TrimPrefix(trimmed, "openai.")
	for _, m := range GetBedrockMantleModels() {
		if m == nil {
			continue
		}
		mID := strings.ToLower(strings.TrimSpace(m.ID))
		if mID == trimmed || mID == stripped || strings.TrimPrefix(mID, "openai.") == stripped {
			return cloneModelInfo(m)
		}
	}
	return nil
}

func upsertModelInfos(models []*ModelInfo, extras ...*ModelInfo) []*ModelInfo {
	if len(extras) == 0 {
		return models
	}

	extraIDs := make(map[string]struct{}, len(extras))
	extraList := make([]*ModelInfo, 0, len(extras))
	for _, extra := range extras {
		if extra == nil {
			continue
		}
		id := strings.TrimSpace(extra.ID)
		if id == "" {
			continue
		}
		key := strings.ToLower(id)
		if _, exists := extraIDs[key]; exists {
			continue
		}
		extraIDs[key] = struct{}{}
		extraList = append(extraList, cloneModelInfo(extra))
	}

	if len(extraList) == 0 {
		return models
	}

	filtered := make([]*ModelInfo, 0, len(models)+len(extraList))
	for _, model := range models {
		if model == nil {
			continue
		}
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		if _, exists := extraIDs[strings.ToLower(id)]; exists {
			continue
		}
		filtered = append(filtered, model)
	}

	filtered = append(filtered, extraList...)
	return filtered
}

// cloneModelInfos returns a shallow copy of the slice with each element deep-cloned.
func cloneModelInfos(models []*ModelInfo) []*ModelInfo {
	if len(models) == 0 {
		return nil
	}
	out := make([]*ModelInfo, len(models))
	for i, m := range models {
		out[i] = cloneModelInfo(m)
	}
	return out
}

// GetStaticModelDefinitionsByChannel returns static model definitions for a given channel/provider.
// It returns nil when the channel is unknown.
//
// Supported channels:
//   - claude
//   - gemini
//   - gemini-interactions
//   - vertex
//   - aistudio
//   - codex
//   - kimi
//   - antigravity
//   - xai
func GetStaticModelDefinitionsByChannel(channel string) []*ModelInfo {
	key := strings.ToLower(strings.TrimSpace(channel))
	switch key {
	case "claude":
		return GetClaudeModels()
	case "gemini":
		return GetGeminiModels()
	case "gemini-interactions":
		return GetGeminiModels()
	case "vertex":
		return GetGeminiVertexModels()
	case "aistudio":
		return GetAIStudioModels()
	case "codex":
		return GetCodexProModels()
	case "kimi":
		return GetKimiModels()
	case "antigravity":
		return GetAntigravityModels()
	case "xai", "x-ai", "grok":
		return GetXAIModels()
	case "bedrock-mantle", "mantle":
		return GetBedrockMantleModels()
	default:
		return nil
	}
}

// LookupStaticModelInfo searches all static model definitions for a model by ID.
// Returns nil if no matching model is found.
func LookupStaticModelInfo(modelID string) *ModelInfo {
	if modelID == "" {
		return nil
	}

	data := getModels()
	allModels := [][]*ModelInfo{
		data.Claude,
		data.Gemini,
		data.Vertex,
		data.AIStudio,
		data.CodexPro,
		data.Kimi,
		data.Antigravity,
		data.XAI,
		GetBedrockMantleModels(),
	}
	normalizedTarget := strings.ToLower(strings.TrimSpace(modelID))
	strippedTarget := strings.TrimPrefix(normalizedTarget, "openai.")

	// Exact match pass
	for _, models := range allModels {
		for _, m := range models {
			if m == nil {
				continue
			}
			if strings.ToLower(strings.TrimSpace(m.ID)) == normalizedTarget {
				return cloneModelInfo(m)
			}
		}
	}

	// Prefix-stripped fallback pass
	for _, models := range allModels {
		for _, m := range models {
			if m == nil {
				continue
			}
			mID := strings.ToLower(strings.TrimSpace(m.ID))
			if mID == strippedTarget || strings.TrimPrefix(mID, "openai.") == strippedTarget {
				return cloneModelInfo(m)
			}
		}
	}

	return nil
}
