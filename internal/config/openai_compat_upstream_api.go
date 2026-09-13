package config

import "strings"

// Upstream API selectors for OpenAI-compatible providers.
const (
	// UpstreamAPIChatCompletions routes requests to POST {base-url}/chat/completions.
	UpstreamAPIChatCompletions = "chat-completions"
	// UpstreamAPIResponses routes requests to POST {base-url}/responses.
	UpstreamAPIResponses = "responses"
)

// NormalizeUpstreamAPI resolves a configured upstream API selector.
// The second result reports whether the value named a known API.
func NormalizeUpstreamAPI(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", false
	case UpstreamAPIChatCompletions, "chat", "chat_completions", "chatcompletions", "completions":
		return UpstreamAPIChatCompletions, true
	case UpstreamAPIResponses, "openai-responses", "openai_responses":
		return UpstreamAPIResponses, true
	default:
		return "", false
	}
}

// UpstreamAPIForModel resolves the upstream API for a request.
// A model-level selector wins over the provider selector, and an unset or
// unrecognized selector falls back to Chat Completions.
func (c *OpenAICompatibility) UpstreamAPIForModel(models ...string) string {
	if c == nil {
		return UpstreamAPIChatCompletions
	}
	for _, model := range models {
		entry := c.findModelEntry(model)
		if entry == nil {
			continue
		}
		if api, ok := NormalizeUpstreamAPI(entry.UpstreamAPI); ok {
			return api
		}
	}
	if api, ok := NormalizeUpstreamAPI(c.UpstreamAPI); ok {
		return api
	}
	return UpstreamAPIChatCompletions
}

// findModelEntry returns the configured model matching an upstream name or alias.
func (c *OpenAICompatibility) findModelEntry(model string) *OpenAICompatibilityModel {
	model = strings.TrimSpace(model)
	if c == nil || model == "" {
		return nil
	}
	for i := range c.Models {
		if strings.EqualFold(model, strings.TrimSpace(c.Models[i].Name)) {
			return &c.Models[i]
		}
	}
	for i := range c.Models {
		if strings.EqualFold(model, strings.TrimSpace(c.Models[i].Alias)) {
			return &c.Models[i]
		}
	}
	return nil
}
