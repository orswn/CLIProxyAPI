package translator

import (
	"bytes"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// upstreamUsageCostFields lists the vendor usage fields that report the price of
// a request. They are not part of any provider schema that a translator
// rebuilds, so they are copied from the upstream body after translation.
var upstreamUsageCostFields = []string{"cost", "cost_details", "total_cost", "is_byok"}

// usageContainerPaths lists the usage object names used by the supported client
// schemas. OpenAI, Claude and Responses use "usage"; Gemini uses "usageMetadata".
var usageContainerPaths = []string{"usage", "usageMetadata"}

// preserveUpstreamUsageCost copies upstream cost reporting into a translated
// body. A translator rebuilds the usage object from token fields it knows, so a
// provider that prices the request would otherwise lose that number on every
// schema change. Bodies that report no cost are returned unchanged.
func preserveUpstreamUsageCost(upstream, translated []byte) []byte {
	if len(upstream) == 0 || len(translated) == 0 {
		return translated
	}
	upstreamPayload, ok := sseJSONPayload(upstream)
	if !ok {
		return translated
	}
	values := upstreamUsageCostValues(upstreamPayload)
	if len(values) == 0 {
		return translated
	}
	start, end, ok := sseJSONPayloadBounds(translated)
	if !ok {
		return translated
	}
	payload := translated[start:end]
	usagePath := ""
	for _, path := range usageContainerPaths {
		if gjson.GetBytes(payload, path).IsObject() {
			usagePath = path
			break
		}
	}
	if usagePath == "" {
		return translated
	}
	changed := false
	for field, raw := range values {
		target := usagePath + "." + field
		if gjson.GetBytes(payload, target).Exists() {
			continue
		}
		updated, errSet := sjson.SetRawBytes(payload, target, raw)
		if errSet != nil {
			continue
		}
		payload = updated
		changed = true
	}
	if !changed {
		return translated
	}
	merged := make([]byte, 0, start+len(payload)+len(translated)-end)
	merged = append(merged, translated[:start]...)
	merged = append(merged, payload...)
	merged = append(merged, translated[end:]...)
	return merged
}

// upstreamUsageCostValues collects the raw cost fields reported by an upstream body.
func upstreamUsageCostValues(payload []byte) map[string][]byte {
	var values map[string][]byte
	for _, usagePath := range usageContainerPaths {
		usage := gjson.GetBytes(payload, usagePath)
		if !usage.IsObject() {
			continue
		}
		for _, field := range upstreamUsageCostFields {
			node := usage.Get(field)
			if !node.Exists() || node.Type == gjson.Null {
				continue
			}
			if values == nil {
				values = make(map[string][]byte, len(upstreamUsageCostFields))
			}
			if _, seen := values[field]; !seen {
				values[field] = []byte(node.Raw)
			}
		}
	}
	return values
}

// sseJSONPayload returns the JSON object carried by a body or SSE frame.
func sseJSONPayload(body []byte) ([]byte, bool) {
	start, end, ok := sseJSONPayloadBounds(body)
	if !ok {
		return nil, false
	}
	return body[start:end], true
}

// sseJSONPayloadBounds locates the JSON object inside a raw body or an SSE frame
// so an edited payload can be written back without losing the frame envelope.
func sseJSONPayloadBounds(body []byte) (int, int, bool) {
	start := bytes.IndexByte(body, '{')
	if start < 0 {
		return 0, 0, false
	}
	end := bytes.LastIndexByte(body, '}')
	if end < start {
		return 0, 0, false
	}
	end++
	if !gjson.ValidBytes(body[start:end]) {
		return 0, 0, false
	}
	return start, end, true
}
