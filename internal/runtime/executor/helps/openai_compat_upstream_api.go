package helps

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

// OpenAICompatResponsesTerminalEvents lists Responses stream events that end a stream.
var openAICompatResponsesTerminalEvents = map[string]struct{}{
	"response.completed":  {},
	"response.failed":     {},
	"response.incomplete": {},
}

// UseOpenAICompatResponsesUpstream reports whether a request must be sent to the
// provider Responses endpoint instead of Chat Completions. It returns false when
// the configured selector is Chat Completions, and also when no translator can
// carry the client schema to or from the Responses schema.
func UseOpenAICompatResponsesUpstream(compat *config.OpenAICompatibility, from, responseFormat sdktranslator.Format, stream bool, models ...string) bool {
	if compat == nil {
		return false
	}
	if compat.UpstreamAPIForModel(models...) != config.UpstreamAPIResponses {
		return false
	}
	return OpenAICompatResponsesSchemaSupported(from, responseFormat, stream)
}

// OpenAICompatResponsesSchemaSupported reports whether the Responses schema can
// carry a request from the client schema and carry the reply back.
// Identical schemas need no translator and pass through unchanged.
func OpenAICompatResponsesSchemaSupported(from, responseFormat sdktranslator.Format, stream bool) bool {
	target := sdktranslator.FormatOpenAIResponse
	if from != target && !sdktranslator.HasRequestTransformer(from, target) {
		return false
	}
	if responseFormat == target {
		return true
	}
	if stream {
		return sdktranslator.HasStreamResponseTransformer(responseFormat, target)
	}
	return sdktranslator.HasNonStreamResponseTransformer(responseFormat, target)
}

// IsOpenAICompatResponsesTerminalEvent reports whether a Responses stream frame
// ends the stream. Responses upstreams close with a terminal event and never
// send the Chat Completions [DONE] sentinel.
func IsOpenAICompatResponsesTerminalEvent(payload []byte, eventName string) bool {
	if name := strings.ToLower(strings.TrimSpace(eventName)); name != "" {
		if _, ok := openAICompatResponsesTerminalEvents[name]; ok {
			return true
		}
	}
	payloadType := strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, "type").String()))
	if payloadType == "" {
		return false
	}
	_, ok := openAICompatResponsesTerminalEvents[payloadType]
	return ok
}
