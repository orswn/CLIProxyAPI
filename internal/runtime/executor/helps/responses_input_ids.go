package helps

import (
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
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
	rebuilt, duplicates := dedupeResponsesInputItems(input)
	if duplicates == 0 {
		return body, 0
	}
	updated, errSet := ojson.SetRawBytes(body, "input", rebuilt)
	if errSet != nil {
		return body, 0
	}
	return updated, duplicates
}

// DedupeResponsesInputItemIDsDoc applies DedupeResponsesInputItemIDs to doc in
// place and returns the number of dropped IDs.
func DedupeResponsesInputItemIDsDoc(doc *ojson.Doc) int {
	if doc == nil {
		return 0
	}
	rebuilt, duplicates := dedupeResponsesInputItems(doc.Get("input"))
	if duplicates == 0 {
		return 0
	}
	if errSet := doc.SetRawBytes("input", rebuilt); errSet != nil {
		return 0
	}
	return duplicates
}

func dedupeResponsesInputItems(input ojson.Result) ([]byte, int) {
	if !input.IsArray() {
		return nil, 0
	}
	items := input.Array()
	if len(items) < 2 {
		return nil, 0
	}

	seen := make(map[string]struct{}, len(items))
	rebuilt := make([]string, 0, len(items))
	duplicates := 0
	for _, item := range items {
		raw := item.Raw
		itemID := item.Get("id")
		if itemID.Type == ojson.String {
			id := itemID.String()
			if id != "" {
				if _, exists := seen[id]; exists {
					if next, errDelete := ojson.DeleteBytes([]byte(raw), "id"); errDelete == nil {
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
		return nil, 0
	}
	return []byte("[" + strings.Join(rebuilt, ",") + "]"), duplicates
}
