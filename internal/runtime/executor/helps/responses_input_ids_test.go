package helps

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestDedupeResponsesInputItemIDs(t *testing.T) {
	body := []byte(`{"model":"m","input":[` +
		`{"type":"message","id":"msg_5","role":"assistant","content":[{"type":"output_text","text":"first"}]},` +
		`{"type":"message","id":"msg_5","role":"assistant","content":[{"type":"output_text","text":"second"}]},` +
		`{"type":"message","id":"msg_6","role":"assistant","content":[{"type":"output_text","text":"third"}]},` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"go on"}]}]}`)

	deduped, duplicates := DedupeResponsesInputItemIDs(body)
	if duplicates != 1 {
		t.Fatalf("duplicates = %d, want 1", duplicates)
	}
	items := gjson.GetBytes(deduped, "input").Array()
	if len(items) != 4 {
		t.Fatalf("items = %d, want 4: %s", len(items), deduped)
	}
	if got := items[0].Get("id").String(); got != "msg_5" {
		t.Errorf("items[0].id = %q, want msg_5", got)
	}
	if items[1].Get("id").Exists() {
		t.Errorf("items[1] kept a duplicate id: %s", items[1].Raw)
	}
	if got := items[1].Get("content.0.text").String(); got != "second" {
		t.Errorf("items[1] content = %q, want second", got)
	}
	if got := items[2].Get("id").String(); got != "msg_6" {
		t.Errorf("items[2].id = %q, want msg_6", got)
	}
}

func TestDedupeResponsesInputItemIDsLeavesCleanPayload(t *testing.T) {
	body := []byte(`{"model":"m","input":[{"type":"message","id":"msg_1","role":"user","content":"hi"},{"type":"message","id":"msg_2","role":"assistant","content":"ok"}]}`)
	deduped, duplicates := DedupeResponsesInputItemIDs(body)
	if duplicates != 0 {
		t.Fatalf("duplicates = %d, want 0", duplicates)
	}
	if string(deduped) != string(body) {
		t.Fatalf("payload changed: %s", deduped)
	}
}

func TestDedupeResponsesInputItemIDsIgnoresNonArrayInput(t *testing.T) {
	body := []byte(`{"model":"m","input":"plain text"}`)
	deduped, duplicates := DedupeResponsesInputItemIDs(body)
	if duplicates != 0 || string(deduped) != string(body) {
		t.Fatalf("payload changed: %s", deduped)
	}
}

func TestDedupeResponsesInputItemIDsKeepsRepeatedCallIDs(t *testing.T) {
	body := []byte(`{"model":"m","input":[` +
		`{"type":"function_call","id":"fc_1","call_id":"call_1","name":"f","arguments":"{}"},` +
		`{"type":"function_call_output","call_id":"call_1","output":"ok"}]}`)
	deduped, duplicates := DedupeResponsesInputItemIDs(body)
	if duplicates != 0 {
		t.Fatalf("duplicates = %d, want 0", duplicates)
	}
	if got := gjson.GetBytes(deduped, "input.1.call_id").String(); got != "call_1" {
		t.Fatalf("call_id = %q, want call_1", got)
	}
}
