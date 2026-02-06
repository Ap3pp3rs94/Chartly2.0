package runner

import (
	"encoding/json"
	"sort"
	"strings"
)

func BuildPrompt(p Profile, sample []any, outputLimit int, outputMaxBytes int) string {
	meta := map[string]any{
		"id":      p.ID,
		"name":    p.Name,
		"version": p.Version,
	}

	rules := make([]map[string]string, 0)
	if len(p.Mapping) > 0 {
		keys := make([]string, 0, len(p.Mapping))
		for k := range p.Mapping { keys = append(keys, k) }
		sort.Strings(keys)
		for _, k := range keys {
			rules = append(rules, map[string]string{"from": k, "to": p.Mapping[k]})
		}
	}

	schema := map[string]any{
		"type": "object",
		"required": []string{"ok","records"},
		"properties": map[string]any{
			"ok": map[string]any{"type":"boolean"},
			"records": map[string]any{"type":"array","items":map[string]any{"type":"object"}},
			"notes": map[string]any{"type":"array"},
			"stats": map[string]any{"type":"object"},
		},
	}

	payload := map[string]any{
		"instruction": "OUTPUT MUST BE STRICT JSON ONLY. NO MARKDOWN.",
		"profile": meta,
		"mapping_rules": rules,
		"sample_records": sample,
		"output_schema": schema,
		"constraints": map[string]any{
			"max_records": outputLimit,
			"max_bytes": outputMaxBytes,
			"deterministic": true,
			"preserve_input_order": true,
			"no_hallucinated_fields": true,
		},
		"required_output": map[string]any{
			"ok": true,
			"records": []any{},
			"notes": []any{},
			"stats": map[string]any{"records_in": len(sample), "records_out": 0},
		},
	}

	b, _ := json.Marshal(payload)
	return string(b)
}

func JoinNotes(items []string) string {
	clean := make([]string, 0, len(items))
	for _, s := range items {
		if strings.TrimSpace(s) != "" { clean = append(clean, s) }
	}
	return strings.Join(clean, "; ")
}
