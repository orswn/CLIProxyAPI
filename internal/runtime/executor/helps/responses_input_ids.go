package helps

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// DedupeResponsesInputItemIDs drops repeated input item IDs so a Responses
// upstream does not reject the whole request. The first item keeps its ID and
// later items with the same ID lose theirs, which makes the upstream treat them
// as new items instead of references to an existing one.
//
// Clients that rebuild conversation history can emit the same synthetic ID for
// several items. Chat Completions upstreams ignore item IDs, so the collision
// only surfaces after a session moves to a Responses upstream.
func DedupeResponsesInputItemIDs(body []byte) ([]byte, int) {
	input := util.GetGJSONBytesNoCopy(body, "input")
	if !input.IsArray() {
		return body, 0
	}
	items := input.Array()
	if len(items) < 2 {
		return body, 0
	}

	seen := make(map[string]struct{}, len(items))
	rebuilt := make([]string, 0, len(items))
	duplicates := 0
	for _, item := range items {
		raw := item.Raw
		itemID := item.Get("id")
		if itemID.Type == gjson.String {
			id := itemID.String()
			if id != "" {
				if _, exists := seen[id]; exists {
					if next, errDelete := sjson.DeleteBytes([]byte(raw), "id"); errDelete == nil {
						raw = string(next)
						duplicates++
					}
				} else {
					seen[id] = struct{}{}
				}
			}
		}
		rebuilt = append(rebuilt, raw)
	}
	if duplicates == 0 {
		return body, 0
	}
	updated, errSet := sjson.SetRawBytes(body, "input", []byte("["+strings.Join(rebuilt, ",")+"]"))
	if errSet != nil {
		return body, 0
	}
	return updated, duplicates
}
