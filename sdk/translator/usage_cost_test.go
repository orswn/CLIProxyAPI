package translator

import (
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestPreserveUpstreamUsageCostCopiesIntoTranslatedBody(t *testing.T) {
	upstream := []byte(`{"id":"chatcmpl_1","usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"cost":0.00123,"cost_details":{"upstream_inference_cost":0.001}}}`)
	translated := []byte(`{"id":"msg_1","type":"message","usage":{"input_tokens":10,"output_tokens":5}}`)

	out := preserveUpstreamUsageCost(upstream, translated)
	if got := gjson.GetBytes(out, "usage.cost").Float(); got != 0.00123 {
		t.Fatalf("usage.cost = %v, want 0.00123: %s", got, out)
	}
	if got := gjson.GetBytes(out, "usage.cost_details.upstream_inference_cost").Float(); got != 0.001 {
		t.Fatalf("usage.cost_details = %s", out)
	}
	if got := gjson.GetBytes(out, "usage.input_tokens").Int(); got != 10 {
		t.Fatalf("token fields changed: %s", out)
	}
}

func TestPreserveUpstreamUsageCostCopiesIntoGeminiUsage(t *testing.T) {
	upstream := []byte(`{"usage":{"total_tokens":15,"cost":0.5}}`)
	translated := []byte(`{"usageMetadata":{"promptTokenCount":10,"totalTokenCount":15}}`)

	out := preserveUpstreamUsageCost(upstream, translated)
	if got := gjson.GetBytes(out, "usageMetadata.cost").Float(); got != 0.5 {
		t.Fatalf("usageMetadata.cost = %v: %s", got, out)
	}
}

func TestPreserveUpstreamUsageCostKeepsSSEFrame(t *testing.T) {
	upstream := []byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\"},\"usage\":{\"input_tokens\":1,\"cost\":0.25}}")
	translated := []byte("data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":2}}")

	out := preserveUpstreamUsageCost(upstream, translated)
	if !strings.HasPrefix(string(out), "data: ") {
		t.Fatalf("frame prefix lost: %s", out)
	}
	if got := gjson.Get(strings.TrimPrefix(string(out), "data: "), "usage.cost").Float(); got != 0.25 {
		t.Fatalf("usage.cost = %v: %s", got, out)
	}
}

func TestPreserveUpstreamUsageCostLeavesBodiesWithoutCost(t *testing.T) {
	upstream := []byte(`{"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	translated := []byte(`{"usage":{"input_tokens":1,"output_tokens":1}}`)

	out := preserveUpstreamUsageCost(upstream, translated)
	if string(out) != string(translated) {
		t.Fatalf("body changed: %s", out)
	}
}

func TestPreserveUpstreamUsageCostKeepsExistingCost(t *testing.T) {
	upstream := []byte(`{"usage":{"cost":0.5}}`)
	translated := []byte(`{"usage":{"cost":0.9}}`)

	out := preserveUpstreamUsageCost(upstream, translated)
	if got := gjson.GetBytes(out, "usage.cost").Float(); got != 0.9 {
		t.Fatalf("usage.cost = %v, want the translated value 0.9", got)
	}
}

func TestPreserveUpstreamUsageCostIgnoresMultiFrameChunks(t *testing.T) {
	upstream := []byte(`{"usage":{"cost":0.5}}`)
	translated := []byte("event: a\ndata: {\"usage\":{\"output_tokens\":1}}\n\nevent: b\ndata: {\"type\":\"done\"}\n\n")

	out := preserveUpstreamUsageCost(upstream, translated)
	if string(out) != string(translated) {
		t.Fatalf("multi-frame chunk changed: %s", out)
	}
}
