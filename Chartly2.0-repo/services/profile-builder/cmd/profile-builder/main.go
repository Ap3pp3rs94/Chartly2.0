package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAddr     = ":8085"
	defaultVersion  = "1.0.0"
	maxBodyBytes    = 2 << 20
	maxFetchBytes   = 4 << 20
	maxFieldsDefault = 200
)

type generateReq struct {
	ProfileID   string      `json:"profile_id"`
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	Source      sourceSpec  `json:"source"`
	SampleJSON  interface{} `json:"sample_json"`
	MaxFields   int         `json:"max_fields"`
}

type sourceSpec struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type generateResp struct {
	ProfileYAML  string   `json:"profile_yaml"`
	MappingCount int      `json:"mapping_count"`
	JoinKeys     []string `json:"join_keys"`
	NumericFields []string `json:"numeric_fields"`
	SourceURL    string   `json:"source_url"`
	SampleUsed   string   `json:"sample_used"` // "provided" | "fetched"
}

func main() {
	addr := getenv("PROFILE_BUILDER_ADDR", defaultAddr)
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodOptions {
			writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "healthy"})
	})

	mux.HandleFunc("/api/profile-builder/generate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		defer r.Body.Close()

		var req generateReq
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
			return
		}

		req.ProfileID = strings.TrimSpace(req.ProfileID)
		req.Name = strings.TrimSpace(req.Name)
		req.Version = strings.TrimSpace(req.Version)
		req.Source.Type = strings.TrimSpace(req.Source.Type)
		req.Source.URL = strings.TrimSpace(req.Source.URL)
		if req.Version == "" {
			req.Version = defaultVersion
		}
		if req.Source.Type == "" {
			req.Source.Type = "http_rest"
		}
		if req.MaxFields <= 0 || req.MaxFields > 1000 {
			req.MaxFields = maxFieldsDefault
		}

		if req.ProfileID == "" {
			writeErr(w, http.StatusBadRequest, "missing_profile_id", "profile_id is required")
			return
		}
		if req.Name == "" {
			req.Name = req.ProfileID
		}
		if req.Source.URL == "" && req.SampleJSON == nil {
			writeErr(w, http.StatusBadRequest, "missing_sample", "sample_json or source.url is required")
			return
		}

		var sample interface{}
		sampleUsed := "provided"
		if req.SampleJSON != nil {
			sample = req.SampleJSON
		} else {
			sampleUsed = "fetched"
			b, err := fetchSample(req.Source.URL)
			if err != nil {
				writeErr(w, http.StatusBadRequest, "fetch_failed", err.Error())
				return
			}
			if err := json.Unmarshal(b, &sample); err != nil {
				writeErr(w, http.StatusBadRequest, "invalid_sample_json", "fetched sample is not valid JSON")
				return
			}
		}

		record := normalizeSample(sample)
		if record == nil {
			writeErr(w, http.StatusBadRequest, "invalid_sample_shape", "sample_json must be object or array of objects")
			return
		}

		paths := flatten(record, "", map[string]interface{}{})
		srcPaths := make([]string, 0, len(paths))
		for k := range paths {
			srcPaths = append(srcPaths, k)
		}
		sort.Strings(srcPaths)
		if len(srcPaths) > req.MaxFields {
			srcPaths = srcPaths[:req.MaxFields]
		}

		mapping := make([]string, 0, len(srcPaths))
		joinKeys := make([]string, 0)
		numericFields := make([]string, 0)

		for _, src := range srcPaths {
			val := paths[src]
			dst := mapPath(src, val)
			mapping = append(mapping, fmt.Sprintf("  %s: %s", src, dst))
			if isNumeric(val) || strings.HasPrefix(dst, "measures.") {
				numericFields = append(numericFields, dst)
			} else {
				joinKeys = append(joinKeys, dst)
			}
		}

		sort.Strings(joinKeys)
		sort.Strings(numericFields)

		yaml := buildYAML(req, mapping)
		resp := generateResp{
			ProfileYAML:   yaml,
			MappingCount:  len(mapping),
			JoinKeys:      joinKeys,
			NumericFields: numericFields,
			SourceURL:     req.Source.URL,
			SampleUsed:    sampleUsed,
		}
		writeJSON(w, http.StatusOK, resp)
	})

	handler := withCORS(mux)
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	logLine("INFO", "starting", "addr=%s", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logLine("ERROR", "listen_failed", "err=%s", err.Error())
		os.Exit(1)
	}
}

func fetchSample(url string) ([]byte, error) {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "Chartly-ProfileBuilder/1.0")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch_error")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("fetch_status_%d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes))
}

func normalizeSample(sample interface{}) map[string]interface{} {
	switch v := sample.(type) {
	case map[string]interface{}:
		return v
	case []interface{}:
		if len(v) == 0 {
			return nil
		}
		if m, ok := v[0].(map[string]interface{}); ok {
			return m
		}
		return nil
	default:
		return nil
	}
}

func flatten(v interface{}, prefix string, out map[string]interface{}) map[string]interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, val := range t {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			flatten(val, key, out)
		}
	case []interface{}:
		if len(t) == 0 {
			out[prefix] = nil
			return out
		}
		flatten(t[0], prefix+"[0]", out)
	default:
		out[prefix] = t
	}
	return out
}

func mapPath(src string, val interface{}) string {
	p := strings.ToLower(src)
	if strings.Contains(p, "state") || strings.Contains(p, "county") || strings.Contains(p, "country") || strings.Contains(p, "city") || strings.Contains(p, "fips") || strings.Contains(p, "zip") {
		return "dims.geo." + cleanPath(src)
	}
	if strings.Contains(p, "date") || strings.Contains(p, "time") || strings.Contains(p, "timestamp") || strings.HasSuffix(p, "_at") {
		return "dims.time." + cleanPath(src)
	}
	if isNumeric(val) {
		return "measures." + cleanPath(src)
	}
	return "dims." + cleanPath(src)
}

func cleanPath(p string) string {
	p = strings.ReplaceAll(p, "[", ".")
	p = strings.ReplaceAll(p, "]", "")
	p = strings.ReplaceAll(p, "..", ".")
	return strings.Trim(p, ".")
}

func isNumeric(v interface{}) bool {
	switch t := v.(type) {
	case float64, float32, int, int64, int32:
		return true
	case json.Number:
		_, err := t.Float64()
		return err == nil
	case string:
		s := strings.TrimSpace(strings.ReplaceAll(t, ",", ""))
		if s == "" {
			return false
		}
		_, err := strconv.ParseFloat(s, 64)
		return err == nil
	default:
		return false
	}
}

func buildYAML(req generateReq, mapping []string) string {
	var buf bytes.Buffer
	buf.WriteString("id: " + req.ProfileID + "\n")
	buf.WriteString("name: " + req.Name + "\n")
	buf.WriteString("version: " + req.Version + "\n")
	buf.WriteString("source:\n")
	buf.WriteString("  type: " + req.Source.Type + "\n")
	buf.WriteString("  url: " + req.Source.URL + "\n")
	buf.WriteString("  auth: none\n")
	buf.WriteString("mapping:\n")
	for _, m := range mapping {
		buf.WriteString(m + "\n")
	}
	return buf.String()
}

func getenv(k, def string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"code": code, "message": msg},
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func logLine(level, msg string, format string, args ...any) {
	ts := time.Now().UTC().Format(time.RFC3339)
	line := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stdout, "%s %s %s %s\n", ts, level, msg, line)
}
