package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	fieldsCacheTTL   = 5 * time.Minute
	fieldsMaxBytes   = 2 << 20
	fieldsHTTPTimeout = 15 * time.Second
	fieldsSampleN    = 5
)

type profileFieldsResp struct {
	ProfileID        string        `json:"profile_id"`
	Name             string        `json:"name"`
	Fields           []profileField `json:"fields"`
	Cached           bool          `json:"cached"`
	ExpiresInSeconds int64         `json:"expires_in_seconds"`
}

type profileField struct {
	Path   string      `json:"path"`
	Label  string      `json:"label"`
	Type   string      `json:"type"`
	Sample interface{} `json:"sample"`
}

type fieldsCacheEntry struct {
	resp      profileFieldsResp
	expiresAt time.Time
	key       string
}

var (
	fieldsCacheMu sync.Mutex
	fieldsCache   = map[string]fieldsCacheEntry{}
)

// GetProfileFields handles GET /api/profiles/{id}/fields
func GetProfileFields(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/profiles/")
	if !strings.HasSuffix(path, "/fields") {
		writeErr(w, http.StatusNotFound, "not_found", "endpoint not found")
		return
	}
	id := strings.TrimSuffix(path, "/fields")
	id = strings.Trim(id, "/")
	if id == "" || strings.Contains(id, "..") {
		writeErr(w, http.StatusBadRequest, "invalid_profile_id", "invalid profile id")
		return
	}

	root, err := findRepoRoot()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "repo_root_not_found", "repo root not found")
		return
	}
	profilePath := filepath.Join(root, "profiles", "government", id+".yaml")
	b, err := os.ReadFile(profilePath)
	if err != nil {
		writeErr(w, http.StatusNotFound, "profile_not_found", "profile not found")
		return
	}

	var p profileYAMLFields
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	if err := dec.Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_profile", "invalid profile yaml")
		return
	}
	if strings.TrimSpace(p.Source.URL) == "" {
		writeErr(w, http.StatusBadRequest, "missing_source_url", "profile source.url is required")
		return
	}

	resolvedURL, err := expandEnvPlaceholders(strings.TrimSpace(p.Source.URL))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "missing_env_var", err.Error())
		return
	}

	cacheKey := id + "|" + resolvedURL
	if resp, ok := fieldsCacheGet(cacheKey); ok {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	sample, err := fetchSample(resolvedURL)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "sample_fetch_failed", "failed to fetch sample data")
		return
	}

	records := normalizeRecords(sample)
	if len(records) == 0 {
		writeErr(w, http.StatusBadRequest, "invalid_sample_shape", "sample is not a JSON object or array")
		return
	}

	fields := inferFields(records)
	resp := profileFieldsResp{
		ProfileID:        firstNonEmpty(strings.TrimSpace(p.ID), id),
		Name:             firstNonEmpty(strings.TrimSpace(p.Name), id),
		Fields:           fields,
		Cached:           false,
		ExpiresInSeconds: int64(fieldsCacheTTL.Seconds()),
	}
	fieldsCacheSet(cacheKey, resp)
	writeJSON(w, http.StatusOK, resp)
}

// ---- Helpers ----

type profileYAMLFields struct {
	ID     string `yaml:"id"`
	Name   string `yaml:"name"`
	Source struct {
		Type string `yaml:"type"`
		URL  string `yaml:"url"`
		Auth string `yaml:"auth"`
	} `yaml:"source"`
}

func findRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for i := 0; i < 8; i++ {
		if isProfilesDir(dir) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", errors.New("repo root not found")
}

func isProfilesDir(root string) bool {
	p := filepath.Join(root, "profiles", "government")
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

var envRe = regexp.MustCompile(`\$\{([A-Za-z0-9_]+)\}`)

func expandEnvPlaceholders(s string) (string, error) {
	matches := envRe.FindAllStringSubmatch(s, -1)
	for _, m := range matches {
		key := m[1]
		val := strings.TrimSpace(os.Getenv(key))
		if val == "" {
			return "", fmt.Errorf("missing env var %s", key)
		}
		s = strings.ReplaceAll(s, "${"+key+"}", val)
	}
	return s, nil
}

func fetchSample(url string) (interface{}, error) {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "Chartly-Gateway/1.0")
	client := &http.Client{Timeout: fieldsHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("http_%d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, fieldsMaxBytes))
	if err != nil {
		return nil, err
	}
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return v, nil
}

func normalizeRecords(v interface{}) []interface{} {
	switch t := v.(type) {
	case []interface{}:
		return t
	case map[string]interface{}:
		for k, v2 := range t {
			if strings.EqualFold(k, "results") {
				if arr, ok := v2.([]interface{}); ok {
					return arr
				}
			}
		}
		return []interface{}{t}
	default:
		return nil
	}
}

type fieldInfo struct {
	Path   string
	Label  string
	Type   string
	Sample interface{}
}

func inferFields(records []interface{}) []profileField {
	acc := map[string]fieldInfo{}
	n := fieldsSampleN
	if len(records) < n {
		n = len(records)
	}
	for i := 0; i < n; i++ {
		if m, ok := records[i].(map[string]interface{}); ok {
			flattenFields(m, "", acc)
		}
	}

	fields := make([]profileField, 0, len(acc))
	for _, v := range acc {
		fields = append(fields, profileField{
			Path:   v.Path,
			Label:  v.Label,
			Type:   v.Type,
			Sample: v.Sample,
		})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Path < fields[j].Path })
	return fields
}

func flattenFields(v interface{}, prefix string, acc map[string]fieldInfo) {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, val := range t {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			flattenFields(val, path, acc)
		}
	case []interface{}:
		if len(t) == 0 {
			addField(acc, prefix, "array", nil)
			return
		}
		// array of objects? flatten first N with normalized [0]
		if isObjectArray(t) {
			for i := 0; i < len(t) && i < fieldsSampleN; i++ {
				m, ok := t[i].(map[string]interface{})
				if !ok {
					continue
				}
				path := prefix + "[0]"
				flattenFields(m, path, acc)
			}
			return
		}
		// scalar array
		addField(acc, prefix, "array", sampleArray(t))
	default:
		typ := inferType(v)
		if typ == "" {
			return
		}
		addField(acc, prefix, typ, v)
	}
}

func isObjectArray(a []interface{}) bool {
	for _, v := range a {
		if _, ok := v.(map[string]interface{}); ok {
			return true
		}
	}
	return false
}

func sampleArray(a []interface{}) interface{} {
	if len(a) == 0 {
		return nil
	}
	return a[0]
}

func addField(acc map[string]fieldInfo, path, typ string, sample interface{}) {
	if path == "" {
		return
	}
	path = normalizeIndex(path)
	existing, ok := acc[path]
	if !ok {
		acc[path] = fieldInfo{
			Path:   path,
			Label:  makeLabel(path),
			Type:   typ,
			Sample: sample,
		}
		return
	}
	// choose stronger type
	if rankType(typ) > rankType(existing.Type) {
		existing.Type = typ
		existing.Sample = sample
	}
	acc[path] = existing
}

func normalizeIndex(path string) string {
	re := regexp.MustCompile(`\[\d+\]`)
	return re.ReplaceAllString(path, "[0]")
}

func inferType(v interface{}) string {
	switch v.(type) {
	case float64, float32, int, int64, int32, json.Number:
		return "number"
	case string:
		return "string"
	case bool:
		return "boolean"
	case []interface{}:
		return "array"
	default:
		return ""
	}
}

func rankType(t string) int {
	switch t {
	case "number":
		return 4
	case "string":
		return 3
	case "boolean":
		return 2
	case "array":
		return 1
	default:
		return 0
	}
}

func makeLabel(path string) string {
	parts := strings.Split(path, ".")
	last := parts[len(parts)-1]
	last = strings.ReplaceAll(last, "[0]", "")
	last = strings.ReplaceAll(last, "_", " ")
	last = strings.ReplaceAll(last, "-", " ")
	words := strings.Fields(last)
	for i := range words {
		if isAllCaps(words[i]) {
			continue
		}
		words[i] = strings.Title(strings.ToLower(words[i]))
	}
	return strings.Join(words, " ")
}

func isAllCaps(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			return false
		}
	}
	return true
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return strings.TrimSpace(a)
	}
	return strings.TrimSpace(b)
}

func fieldsCacheGet(key string) (profileFieldsResp, bool) {
	fieldsCacheMu.Lock()
	defer fieldsCacheMu.Unlock()
	if e, ok := fieldsCache[key]; ok {
		if time.Now().Before(e.expiresAt) {
			resp := e.resp
			resp.Cached = true
			resp.ExpiresInSeconds = int64(time.Until(e.expiresAt).Seconds())
			return resp, true
		}
		delete(fieldsCache, key)
	}
	return profileFieldsResp{}, false
}

func fieldsCacheSet(key string, resp profileFieldsResp) {
	fieldsCacheMu.Lock()
	defer fieldsCacheMu.Unlock()
	fieldsCache[key] = fieldsCacheEntry{
		resp:      resp,
		expiresAt: time.Now().Add(fieldsCacheTTL),
		key:       key,
	}
}
