package runner

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

func ValidateOutput(output string, limit int, maxBytes int) ([]map[string]any, string, error) {
	var root map[string]any
	if err := json.Unmarshal([]byte(output), &root); err != nil {
		return nil, "", err
	}
	okVal, ok := root["ok"].(bool)
	if !ok || !okVal {
		return nil, "", errors.New("output_not_ok")
	}
	recAny, ok := root["records"].([]any)
	if !ok {
		return nil, "", errors.New("records_missing")
	}

	out := make([]map[string]any, 0, len(recAny))
	for _, r := range recAny {
		obj, ok := r.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, coerceNumbers(obj).(map[string]any))
		if len(out) >= limit { break }
	}

	b, _ := canonicalJSON(out)
	if len(b) > maxBytes {
		return nil, "", errors.New("output_too_large")
	}
	return out, hashCanonical(out), nil
}

func coerceNumbers(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range t {
			out[k] = coerceNumbers(val)
		}
		return out
	case []any:
		arr := make([]any, 0, len(t))
		for _, it := range t {
			arr = append(arr, coerceNumbers(it))
		}
		return arr
	case string:
		s := strings.TrimSpace(t)
		if isLeadingZeroNumber(s) {
			return t
		}
		if n, ok := parseNumber(s); ok {
			return n
		}
		return t
	default:
		return v
	}
}

func isLeadingZeroNumber(s string) bool {
	if len(s) >= 2 && s[0] == '0' && s[1] >= '0' && s[1] <= '9' {
		return true
	}
	return false
}

func parseNumber(s string) (float64, bool) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	if isNaN(f) {
		return 0, false
	}
	return f, true
}

func isNaN(f float64) bool { return f != f }
