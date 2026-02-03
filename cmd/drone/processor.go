package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Profile struct {
	ID      string            `yaml:"id" json:"id"`
	Name    string            `yaml:"name" json:"name"`
	Version string            `yaml:"version" json:"version"`
	Source  SourceConfig      `yaml:"source" json:"source"`
	Mapping map[string]string `yaml:"mapping" json:"mapping"`
}

type SourceConfig struct {
	Type string `yaml:"type"` // "http_rest"
	URL  string `yaml:"url"`
	Auth string `yaml:"auth"` // "none"
}

func ProcessProfile(profile Profile) ([]map[string]any, error) {
	if strings.TrimSpace(profile.Source.URL) == "" {
		logProc("missing_source_url profile_id=%s", profile.ID)
		return []map[string]any{}, fmt.Errorf("missing_source_url")
	}

	client := &http.Client{Timeout: 30 * time.Second}

	raw, err := fetchSource(client, profile.Source.URL)
	if err != nil {
		logProc("fetch_failed url=%s err=%s", profile.Source.URL, err.Error())
		return []map[string]any{}, err
	}

	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		logProc("json_parse_failed url=%s err=%s", profile.Source.URL, err.Error())
		return []map[string]any{}, err
	}

	records := normalizeToRecords(parsed)

	out := make([]map[string]any, 0, len(records))
	for _, rec := range records {
		dst := make(map[string]any)

		for srcPath, dstPath := range profile.Mapping {
			srcPath = strings.TrimSpace(srcPath)
			dstPath = strings.TrimSpace(dstPath)
			if srcPath == "" || dstPath == "" {
				continue
			}

			val, ok := getValueByPath(rec, srcPath)
			if !ok {
				continue
			}
			setNestedValue(dst, dstPath, val)
		}

		// If mapping is empty, pass-through objects as best-effort.
		if len(profile.Mapping) == 0 {
			if m, ok := rec.(map[string]any); ok {
				dst = m
			}
		}
		out = append(out, dst)
	}

	return out, nil
}

func fetchSource(client *http.Client, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	// Liberty: BLS timeseries endpoint requires POST; profiles may specify only URL.
	// If host matches api.bls.gov and path includes /timeseries/data/, do POST with minimal payload.
	if strings.EqualFold(u.Host, "api.bls.gov") && strings.Contains(u.Path, "/publicAPI/v2/timeseries/data/") {
		payload := map[string]any{
			"seriesid": []string{"LNS14000000"},
		}
		b, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, rawURL, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Chartly-Drone/1.0")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			return nil, fmt.Errorf("http_status_%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	}

	req, _ := http.NewRequest(http.MethodGet, rawURL, nil)
	req.Header.Set("User-Agent", "Chartly-Drone/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("http_status_%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

func normalizeToRecords(parsed any) []any {
	// Census style: top-level array-of-arrays with header row.
	if arr, ok := parsed.([]any); ok && len(arr) > 0 {
		if isArrayOfArraysWithHeader(arr) {
			return censusToObjects(arr)
		}
		// Standard array
		return arr
	}

	// Object
	if obj, ok := parsed.(map[string]any); ok {
		// If object contains "results" (case-insensitive) as array, use it.
		for k, v := range obj {
			if strings.EqualFold(k, "results") {
				if a, ok := v.([]any); ok {
					return a
				}
			}
		}
		// Else treat object itself as a single record.
		return []any{obj}
	}

	// Unknown type -> single record wrapper
	return []any{parsed}
}

func isArrayOfArraysWithHeader(arr []any) bool {
	if len(arr) < 2 {
		return false
	}
	h, ok := arr[0].([]any)
	if !ok || len(h) == 0 {
		return false
	}
	for _, v := range h {
		if _, ok := v.(string); !ok {
			return false
		}
	}
	// Require at least one data row as []any
	_, ok = arr[1].([]any)
	return ok
}

func censusToObjects(arr []any) []any {
	headerRow := arr[0].([]any)
	headers := make([]string, 0, len(headerRow))
	for _, h := range headerRow {
		headers = append(headers, fmt.Sprintf("%v", h))
	}

	out := make([]any, 0, len(arr)-1)
	for i := 1; i < len(arr); i++ {
		row, ok := arr[i].([]any)
		if !ok {
			continue
		}
		obj := make(map[string]any)
		for j := 0; j < len(headers) && j < len(row); j++ {
			obj[headers[j]] = row[j]
		}
		out = append(out, obj)
	}
	return out
}

// setNestedValue creates nested maps for "a.b.c".
func setNestedValue(obj map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	cur := obj
	for i := 0; i < len(parts); i++ {
		p := strings.TrimSpace(parts[i])
		if p == "" {
			return
		}
		if i == len(parts)-1 {
			cur[p] = value
			return
		}
		next, ok := cur[p]
		if ok {
			if m, ok := next.(map[string]any); ok {
				cur = m
				continue
			}
			// Overwrite non-map deterministically.
		}
		nm := make(map[string]any)
		cur[p] = nm
		cur = nm
	}
}

type tokenKind int

const (
	tokField tokenKind = iota
	tokIndex
)

type pathToken struct {
	kind  tokenKind
	field string
	index int
}

func getValueByPath(obj any, path string) (any, bool) {
	toks, ok := parsePath(path)
	if !ok {
		return nil, false
	}
	cur := obj
	for _, t := range toks {
		switch t.kind {
		case tokField:
			m, ok := cur.(map[string]any)
			if !ok {
				return nil, false
			}
			v, exists := m[t.field]
			if !exists {
				return nil, false
			}
			cur = v
		case tokIndex:
			a, ok := cur.([]any)
			if !ok {
				return nil, false
			}
			if t.index < 0 || t.index >= len(a) {
				return nil, false
			}
			cur = a[t.index]
		default:
			return nil, false
		}
	}
	return cur, true
}

// parsePath supports: a.b[0].c and nested indexes like data[0][1] (treated as sequential).
func parsePath(path string) ([]pathToken, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, false
	}

	out := make([]pathToken, 0, 8)
	i := 0
	for i < len(path) {
		// field name
		if path[i] == '.' {
			i++
			continue
		}
		if path[i] == '[' {
			// index
			j := strings.IndexByte(path[i:], ']')
			if j < 0 {
				return nil, false
			}
			j = i + j
			num := strings.TrimSpace(path[i+1 : j])
			n, err := strconv.Atoi(num)
			if err != nil {
				return nil, false
			}
			out = append(out, pathToken{kind: tokIndex, index: n})
			i = j + 1
			continue
		}

		// read field until '.' or '['
		start := i
		for i < len(path) && path[i] != '.' && path[i] != '[' {
			i++
		}
		field := strings.TrimSpace(path[start:i])
		if field == "" {
			return nil, false
		}
		out = append(out, pathToken{kind: tokField, field: field})
	}
	return out, true
}

func logProc(format string, args ...any) {
	ts := time.Now().UTC().Format(time.RFC3339)
	fmt.Printf("%s WARN processor %s\n", ts, fmt.Sprintf(format, args...))
}
