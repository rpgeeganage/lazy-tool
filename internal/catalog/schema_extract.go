package catalog

import (
	"encoding/json"
	"sort"
	"strings"
)

// SchemaArgNames returns distinct JSON Schema property keys (recursive, shallow + nested objects).
func SchemaArgNames(schemaJSON string) []string {
	if schemaJSON == "" || schemaJSON == "{}" {
		return nil
	}
	var root any
	if err := json.Unmarshal([]byte(schemaJSON), &root); err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	walkSchema(root, seen)
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func SchemaSignals(schemaJSON string) []string {
	args := SchemaArgNames(schemaJSON)
	seen := map[string]struct{}{}
	for _, arg := range args {
		low := strings.ToLower(arg)
		switch {
		case strings.Contains(low, "query") || strings.Contains(low, "search") || strings.Contains(low, "filter"):
			seen["searches"] = struct{}{}
		case strings.Contains(low, "url") || strings.Contains(low, "uri"):
			seen["fetches"] = struct{}{}
		case strings.Contains(low, "content"):
			seen["content"] = struct{}{}
		case strings.Contains(low, "path"):
			seen["path"] = struct{}{}
		case strings.HasSuffix(low, "id") || low == "id":
			seen["reads"] = struct{}{}
		}
	}
	if _, hasPath := seen["path"]; hasPath {
		if _, hasContent := seen["content"]; hasContent {
			seen["writes"] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func walkSchema(v any, seen map[string]struct{}) {
	switch x := v.(type) {
	case map[string]any:
		if props, ok := x["properties"].(map[string]any); ok {
			for name := range props {
				if name != "" && name != "_meta" {
					seen[name] = struct{}{}
					walkSchema(props[name], seen)
				}
			}
		}
		if items, ok := x["items"]; ok {
			walkSchema(items, seen)
		}
		for _, k := range []string{"allOf", "anyOf", "oneOf"} {
			if arr, ok := x[k].([]any); ok {
				for _, el := range arr {
					walkSchema(el, seen)
				}
			}
		}
	}
}
