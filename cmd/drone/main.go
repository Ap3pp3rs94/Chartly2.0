package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	httpTimeout      = 30 * time.Second
	maxBodyBytes     = 8 << 20
	defaultInterval  = 5 * time.Minute
	retryMaxAttempts = 3
)

type registerResponse struct {
	ID               string   `json:"id"`
	Status           string   `json:"status"`
	AssignedProfiles []string `json:"assigned_profiles"`
}

type profileEnvelope struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Version  string         `json:"version"`
	Content  string         `json:"content"`
	Enabled  *bool          `json:"enabled,omitempty"`
	Interval string         `json:"interval,omitempty"`
	Jitter   string         `json:"jitter,omitempty"`
	Limits   *profileLimits `json:"limits,omitempty"`
}

type profileLimits struct {
	MaxRecords *int `json:"max_records,omitempty"`
	MaxPages   *int `json:"max_pages,omitempty"`
	MaxBytes   *int `json:"max_bytes,omitempty"`
}

type workResponse struct {
	DroneID  string   `json:"drone_id"`
	Profiles []string `json:"profiles"`
}

type runReport struct {
	RunID      string `json:"run_id"`
	DroneID    string `json:"drone_id"`
	ProfileID  string `json:"profile_id"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	Status     string `json:"status"`
	RowsOut    int    `json:"rows_out"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type sourceSpec struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Description  string            `json:"description"`
	Source       SourceConfig      `json:"source"`
	Schedule     *scheduleSpec     `json:"schedule,omitempty"`
	Limits       *limitsSpec       `json:"limits,omitempty"`
	MappingHints map[string]string `json:"mapping_hints,omitempty"`
}

type scheduleSpec struct {
	Enabled  *bool  `json:"enabled,omitempty"`
	Interval string `json:"interval,omitempty"`
	Jitter   string `json:"jitter,omitempty"`
}

type limitsSpec struct {
	MaxRecords *int `json:"max_records,omitempty"`
	MaxPages   *int `json:"max_pages,omitempty"`
	MaxBytes   *int `json:"max_bytes,omitempty"`
}

type profileOut struct {
	ID          string            `yaml:"id"`
	Name        string            `yaml:"name"`
	Version     string            `yaml:"version"`
	Description string            `yaml:"description,omitempty"`
	Source      SourceConfig      `yaml:"source"`
	Schedule    *scheduleOut      `yaml:"schedule,omitempty"`
	Limits      *limitsOut        `yaml:"limits,omitempty"`
	Mapping     map[string]string `yaml:"mapping"`
}

type scheduleOut struct {
	Enabled  bool   `yaml:"enabled"`
	Interval string `yaml:"interval"`
	Jitter   string `yaml:"jitter"`
}

type limitsOut struct {
	MaxRecords int `yaml:"max_records"`
	MaxPages   int `yaml:"max_pages"`
	MaxBytes   int `yaml:"max_bytes"`
}

func main() {
	controlPlane := strings.TrimSpace(os.Getenv("CONTROL_PLANE"))
	if controlPlane == "" {
		fmt.Fprintln(os.Stderr, "missing CONTROL_PLANE")
		os.Exit(1)
	}
	controlPlane = strings.TrimRight(controlPlane, "/")

	droneID := strings.TrimSpace(os.Getenv("DRONE_ID"))
	if droneID == "" {
		droneID = mustUUIDv4()
	}

	interval := defaultInterval
	if v := strings.TrimSpace(os.Getenv("PROCESS_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			interval = d
		}
	}

	client := &http.Client{Timeout: httpTimeout}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logLine("WARN", droneID, "shutdown_signal_received")
		cancel()
	}()

	var regResp registerResponse
	if err := doJSON(ctx, client, http.MethodPost, controlPlane+"/api/drones/register",
		map[string]any{"id": droneID}, &regResp); err != nil {
		logLine("ERROR", droneID, "register_failed err=%s", err.Error())
		os.Exit(1)
	}

	assigned := regResp.AssignedProfiles
	logLine("INFO", droneID, "registered profiles_assigned=%d", len(assigned))

	// Advanced profile generator + auto-mapper (best quality). Runs once on startup.
	if err := buildProfiles(ctx, client, controlPlane, droneID); err != nil {
		logLine("WARN", droneID, "profile_build_failed err=%s", err.Error())
	}

	lastRun := make(map[string]time.Time)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	if err := iteration(ctx, client, controlPlane, droneID, assigned, lastRun); err != nil {
		logLine("WARN", droneID, "iteration_completed_with_errors err=%s", err.Error())
	}

	for {
		select {
		case <-ctx.Done():
			logLine("INFO", droneID, "shutdown_complete")
			return
		case <-ticker.C:
			if err := iteration(ctx, client, controlPlane, droneID, assigned, lastRun); err != nil {
				logLine("WARN", droneID, "iteration_completed_with_errors err=%s", err.Error())
			}
		}
	}
}

func iteration(ctx context.Context, client *http.Client, cp, droneID string, assigned []string, lastRun map[string]time.Time) error {
	var iterErr error
	executed := 0
	skipped := 0

	forced := fetchWorkQueue(ctx, client, cp, droneID)

	for _, pid := range assigned {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		forcedRun := forced[pid]

		var env profileEnvelope
		if err := doJSON(ctx, client, http.MethodGet, cp+"/api/profiles/"+pid, nil, &env); err != nil {
			iterErr = joinErr(iterErr, fmt.Errorf("profile_get_failed id=%s err=%w", pid, err))
			continue
		}

		if env.Enabled != nil && !*env.Enabled && !forcedRun {
			skipped++
			continue
		}

		if !forcedRun {
			if due, ok := isDue(env, lastRun[pid], droneID, pid); ok && !due {
				skipped++
				continue
			}
		}

		runID := mustUUIDv4()
		started := time.Now().UTC()

		var p Profile
		if err := yaml.Unmarshal([]byte(env.Content), &p); err != nil {
			iterErr = joinErr(iterErr, fmt.Errorf("profile_yaml_decode_failed id=%s err=%w", pid, err))
			reportRun(ctx, client, cp, runID, droneID, pid, started, time.Now().UTC(), "failed", 0, time.Since(started).Milliseconds(), "invalid_profile_yaml")
			continue
		}

		results, err := ProcessProfile(p)
		if err != nil {
			iterErr = joinErr(iterErr, fmt.Errorf("process_failed id=%s err=%w", pid, err))
			reportRun(ctx, client, cp, runID, droneID, pid, started, time.Now().UTC(), "failed", 0, time.Since(started).Milliseconds(), capError(err.Error()))
			continue
		}

		payload := map[string]any{
			"drone_id":   droneID,
			"profile_id": pid,
			"run_id":     runID,
			"data":       results,
		}
		var resp any
		if err := doJSON(ctx, client, http.MethodPost, cp+"/api/results", payload, &resp); err != nil {
			iterErr = joinErr(iterErr, fmt.Errorf("results_post_failed id=%s err=%w", pid, err))
			reportRun(ctx, client, cp, runID, droneID, pid, started, time.Now().UTC(), "partial", len(results), time.Since(started).Milliseconds(), capError(err.Error()))
			continue
		}

		finished := time.Now().UTC()
		duration := finished.Sub(started).Milliseconds()
		reportRun(ctx, client, cp, runID, droneID, pid, started, finished, "succeeded", len(results), duration, "")

		lastRun[pid] = finished
		executed++
	}

	var hbResp any
	if err := doJSON(ctx, client, http.MethodPost, cp+"/api/drones/heartbeat", map[string]any{"id": droneID}, &hbResp); err != nil {
		iterErr = joinErr(iterErr, fmt.Errorf("heartbeat_failed err=%w", err))
	}

	logLine("INFO", droneID, "executed=%d skipped=%d heartbeat=sent", executed, skipped)
	return iterErr
}

func buildProfiles(ctx context.Context, client *http.Client, cp, droneID string) error {
	sources, err := loadSourceSpecs()
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return nil
	}

	apiKey := strings.TrimSpace(os.Getenv("CHARTLY_REGISTRY_API_KEY"))
	allowOverwrite := strings.EqualFold(strings.TrimSpace(os.Getenv("CHARTLY_PROFILE_OVERWRITE")), "1")

	for _, spec := range sources {
		id := strings.TrimSpace(spec.ID)
		if id == "" || strings.Contains(id, "..") || strings.ContainsAny(id, "\\/") {
			logLine("WARN", droneID, "profile_source_invalid_id id=%s", id)
			continue
		}

		if !allowOverwrite {
			if exists := profileExists(ctx, client, cp, id); exists {
				continue
			}
		}

		if strings.TrimSpace(spec.Source.URL) == "" {
			logLine("WARN", droneID, "profile_source_missing_url id=%s", id)
			continue
		}

		expandedURL, err := ExpandEnvPlaceholders(spec.Source.URL)
		if err != nil {
			logLine("WARN", droneID, "profile_source_env_missing id=%s err=%s", id, err.Error())
			continue
		}

		raw, err := fetchSource(client, expandedURL)
		if err != nil {
			logLine("WARN", droneID, "profile_source_fetch_failed id=%s host=%s err=%s", id, safeHost(expandedURL), err.Error())
			continue
		}

		var parsed any
		if err := json.Unmarshal(raw, &parsed); err != nil {
			logLine("WARN", droneID, "profile_source_invalid_json id=%s err=%s", id, err.Error())
			continue
		}

		records := normalizeToRecords(parsed)
		mapping := autoMap(records, spec.MappingHints)
		if len(mapping) == 0 {
			logLine("WARN", droneID, "profile_mapping_empty id=%s", id)
			continue
		}

		p := profileOut{
			ID:          id,
			Name:        firstNonEmpty(spec.Name, id),
			Version:     firstNonEmpty(spec.Version, "1.0.0"),
			Description: strings.TrimSpace(spec.Description),
			Source:      spec.Source,
			Schedule:    defaultSchedule(spec.Schedule),
			Limits:      defaultLimits(spec.Limits),
			Mapping:     mapping,
		}

		yamlBytes, err := buildProfileYAML(p)
		if err != nil {
			logLine("WARN", droneID, "profile_yaml_build_failed id=%s err=%s", id, err.Error())
			continue
		}

		if apiKey == "" {
			logLine("WARN", droneID, "profile_post_skipped_missing_api_key id=%s", id)
			continue
		}

		req := map[string]any{
			"id":      p.ID,
			"name":    p.Name,
			"version": p.Version,
			"content": string(yamlBytes),
		}

		if err := postProfile(ctx, client, cp, apiKey, req); err != nil {
			logLine("WARN", droneID, "profile_post_failed id=%s err=%s", id, err.Error())
			continue
		}
	}

	return nil
}

func loadSourceSpecs() ([]sourceSpec, error) {
	if s := strings.TrimSpace(os.Getenv("CHARTLY_PROFILE_SOURCES")); s != "" {
		return parseSourceSpecs([]byte(s))
	}

	path := strings.TrimSpace(os.Getenv("CHARTLY_PROFILE_SOURCES_FILE"))
	if path == "" {
		path = "/app/profiles/sources.json"
	}

	candidates := []string{path, "./profiles/sources.json"}
	for _, p := range candidates {
		if !fileExists(p) {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		return parseSourceSpecs(b)
	}
	return nil, nil
}

func parseSourceSpecs(b []byte) ([]sourceSpec, error) {
	var out []sourceSpec
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func fileExists(p string) bool {
	if p == "" {
		return false
	}
	if !filepath.IsAbs(p) {
		if _, err := os.Stat(p); err == nil {
			return true
		}
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func profileExists(ctx context.Context, client *http.Client, cp, id string) bool {
	var out any
	err := doJSON(ctx, client, http.MethodGet, cp+"/api/profiles/"+id, nil, &out)
	return err == nil
}

func postProfile(ctx context.Context, client *http.Client, cp, apiKey string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cp+"/api/profiles", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("User-Agent", userAgent())

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("http_error status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func defaultSchedule(in *scheduleSpec) *scheduleOut {
	out := &scheduleOut{
		Enabled:  true,
		Interval: "6h",
		Jitter:   "30s",
	}
	if in == nil {
		return out
	}
	if in.Enabled != nil {
		out.Enabled = *in.Enabled
	}
	if strings.TrimSpace(in.Interval) != "" {
		out.Interval = in.Interval
	}
	if strings.TrimSpace(in.Jitter) != "" {
		out.Jitter = in.Jitter
	}
	return out
}

func defaultLimits(in *limitsSpec) *limitsOut {
	out := &limitsOut{MaxRecords: 5000, MaxPages: 50, MaxBytes: 1048576}
	if in == nil {
		return out
	}
	if in.MaxRecords != nil && *in.MaxRecords > 0 {
		out.MaxRecords = *in.MaxRecords
	}
	if in.MaxPages != nil && *in.MaxPages > 0 {
		out.MaxPages = *in.MaxPages
	}
	if in.MaxBytes != nil && *in.MaxBytes > 0 {
		out.MaxBytes = *in.MaxBytes
	}
	return out
}

func autoMap(records []any, hints map[string]string) map[string]string {
	flat := make(map[string]any)
	for _, r := range records {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		flattenRecord(m, "", flat)
		if len(flat) > 0 {
			break
		}
	}

	mapping := make(map[string]string)
	keys := make([]string, 0, len(flat))
	for k := range flat {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if hints != nil {
			if v, ok := hints[k]; ok && strings.TrimSpace(v) != "" {
				mapping[k] = v
				continue
			}
		}
		val := flat[k]
		dest := autoDestPath(k, val)
		if dest == "" {
			continue
		}
		mapping[k] = dest
	}
	return mapping
}

func flattenRecord(v any, prefix string, out map[string]any) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			next := k
			if prefix != "" {
				next = prefix + "." + k
			}
			flattenRecord(t[k], next, out)
		}
	case []any:
		if len(t) == 0 {
			return
		}
		// sample first element for mapping
		flattenRecord(t[0], prefix+"[0]", out)
	default:
		if prefix != "" {
			out[prefix] = t
		}
	}
}

func autoDestPath(src string, val any) string {
	p := normalizePath(src)
	if p == "" {
		return ""
	}

	lp := strings.ToLower(p)
	if strings.Contains(lp, "year") && isNumberish(val) {
		return "dims.time.year"
	}
	if strings.Contains(lp, "date") || strings.Contains(lp, "timestamp") || strings.Contains(lp, "occurred") {
		return "dims.time.occurred_at"
	}

	if isNumberish(val) {
		return "measures." + p
	}
	return "dims." + p
}

func normalizePath(src string) string {
	// convert [0] to .0, then normalize to safe tokens
	s := strings.ReplaceAll(src, "[", ".")
	s = strings.ReplaceAll(s, "]", "")
	s = strings.Trim(s, ".")
	s = strings.ToLower(s)

	parts := strings.Split(s, ".")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = sanitizeToken(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, ".")
}

func sanitizeToken(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
			continue
		}
		if r == '-' || r == ' ' {
			b.WriteRune('_')
		}
	}
	out := b.String()
	out = strings.Trim(out, "_")
	return out
}

func isNumberish(v any) bool {
	switch t := v.(type) {
	case float64, float32, int, int64, int32, uint64, uint32, uint:
		return true
	case string:
		if t == "" {
			return false
		}
		_, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return err == nil
	default:
		return false
	}
}

func buildProfileYAML(p profileOut) ([]byte, error) {
	root := &yaml.Node{Kind: yaml.MappingNode}
	addKV := func(k string, v *yaml.Node) {
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, v)
	}

	addKV("id", &yaml.Node{Kind: yaml.ScalarNode, Value: p.ID})
	addKV("name", &yaml.Node{Kind: yaml.ScalarNode, Value: p.Name})
	addKV("version", &yaml.Node{Kind: yaml.ScalarNode, Value: p.Version})
	if strings.TrimSpace(p.Description) != "" {
		addKV("description", &yaml.Node{Kind: yaml.ScalarNode, Value: p.Description})
	}

	// source
	source := &yaml.Node{Kind: yaml.MappingNode}
	source.Content = append(source.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "type"}, &yaml.Node{Kind: yaml.ScalarNode, Value: p.Source.Type},
		&yaml.Node{Kind: yaml.ScalarNode, Value: "url"}, &yaml.Node{Kind: yaml.ScalarNode, Value: p.Source.URL},
		&yaml.Node{Kind: yaml.ScalarNode, Value: "auth"}, &yaml.Node{Kind: yaml.ScalarNode, Value: p.Source.Auth},
	)
	addKV("source", source)

	// schedule
	if p.Schedule != nil {
		s := &yaml.Node{Kind: yaml.MappingNode}
		s.Content = append(s.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "enabled"}, &yaml.Node{Kind: yaml.ScalarNode, Value: strconv.FormatBool(p.Schedule.Enabled)},
			&yaml.Node{Kind: yaml.ScalarNode, Value: "interval"}, &yaml.Node{Kind: yaml.ScalarNode, Value: p.Schedule.Interval},
			&yaml.Node{Kind: yaml.ScalarNode, Value: "jitter"}, &yaml.Node{Kind: yaml.ScalarNode, Value: p.Schedule.Jitter},
		)
		addKV("schedule", s)
	}

	// limits
	if p.Limits != nil {
		l := &yaml.Node{Kind: yaml.MappingNode}
		l.Content = append(l.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "max_records"}, &yaml.Node{Kind: yaml.ScalarNode, Value: strconv.Itoa(p.Limits.MaxRecords)},
			&yaml.Node{Kind: yaml.ScalarNode, Value: "max_pages"}, &yaml.Node{Kind: yaml.ScalarNode, Value: strconv.Itoa(p.Limits.MaxPages)},
			&yaml.Node{Kind: yaml.ScalarNode, Value: "max_bytes"}, &yaml.Node{Kind: yaml.ScalarNode, Value: strconv.Itoa(p.Limits.MaxBytes)},
		)
		addKV("limits", l)
	}

	// mapping (sorted)
	mapping := &yaml.Node{Kind: yaml.MappingNode}
	keys := make([]string, 0, len(p.Mapping))
	for k := range p.Mapping {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: k},
			&yaml.Node{Kind: yaml.ScalarNode, Value: p.Mapping[k]},
		)
	}
	addKV("mapping", mapping)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		_ = enc.Close()
		return nil, err
	}
	_ = enc.Close()

	out := buf.Bytes()
	if len(out) == 0 || out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return out, nil
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func fetchWorkQueue(ctx context.Context, client *http.Client, cp, droneID string) map[string]bool {
	out := make(map[string]bool)
	var wr workResponse
	err := doJSON(ctx, client, http.MethodGet, cp+"/api/drones/"+droneID+"/work", nil, &wr)
	if err != nil {
		if strings.Contains(err.Error(), "http_error status=404") {
			return out
		}
		return out
	}
	for _, p := range wr.Profiles {
		out[strings.TrimSpace(p)] = true
	}
	return out
}

func isDue(env profileEnvelope, last time.Time, droneID, profileID string) (bool, bool) {
	if env.Interval == "" {
		return true, true
	}
	d, err := time.ParseDuration(env.Interval)
	if err != nil || d <= 0 {
		return true, true
	}

	jitter := time.Duration(0)
	if env.Jitter != "" {
		if jd, err := time.ParseDuration(env.Jitter); err == nil && jd > 0 {
			jitter = jd
		}
	}

	if last.IsZero() {
		return true, true
	}

	next := last.Add(d + deterministicJitter(droneID, profileID, jitter))
	return time.Now().UTC().After(next), true
}

func deterministicJitter(droneID, profileID string, window time.Duration) time.Duration {
	if window <= 0 {
		return 0
	}
	h := sha256.Sum256([]byte(droneID + "|" + profileID))
	v := int64(binaryToInt(h[:8]))
	if v < 0 {
		v = -v
	}
	return time.Duration(v % int64(window))
}

func binaryToInt(b []byte) int64 {
	var v int64
	for i := 0; i < len(b); i++ {
		v = (v << 8) | int64(b[i])
	}
	return v
}

func reportRun(ctx context.Context, client *http.Client, cp, runID, droneID, profileID string, started, finished time.Time, status string, rows int, durationMs int64, errMsg string) {
	r := runReport{
		RunID:      runID,
		DroneID:    droneID,
		ProfileID:  profileID,
		StartedAt:  started.Format(time.RFC3339),
		FinishedAt: finished.Format(time.RFC3339),
		Status:     status,
		RowsOut:    rows,
		DurationMs: durationMs,
		Error:      capError(errMsg),
	}
	var resp any
	_ = doJSON(ctx, client, http.MethodPost, cp+"/api/runs", r, &resp)
}

func capError(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 2048 {
		return s[:2048]
	}
	return s
}

func doJSON(ctx context.Context, client *http.Client, method, url string, body any, out any) error {
	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyBytes = b
	}

	var lastErr error
	backoff := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

	for attempt := 1; attempt <= retryMaxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(bodyBytes))
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", userAgent())
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < retryMaxAttempts {
				sleepWithContext(ctx, backoff[attempt-1])
				continue
			}
			return lastErr
		}

		b, rerr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		_ = resp.Body.Close()
		if rerr != nil {
			lastErr = rerr
			if attempt < retryMaxAttempts {
				sleepWithContext(ctx, backoff[attempt-1])
				continue
			}
			return lastErr
		}

		if resp.StatusCode >= 500 && attempt < retryMaxAttempts {
			lastErr = fmt.Errorf("server_error status=%d", resp.StatusCode)
			sleepWithContext(ctx, backoff[attempt-1])
			continue
		}
		if resp.StatusCode/100 != 2 {
			return fmt.Errorf("http_error status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(b)))
		}

		if out != nil {
			if err := json.Unmarshal(b, out); err != nil {
				return err
			}
		}
		return nil
	}

	return lastErr
}

func sleepWithContext(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return
	case <-t.C:
		return
	}
}

func mustUUIDv4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	s := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", s[0:8], s[8:12], s[12:16], s[16:20], s[20:32])
}

func logLine(level, droneID, format string, args ...any) {
	ts := time.Now().UTC().Format(time.RFC3339)
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("%s %s drone_id=%s %s\n", ts, level, droneID, msg)
}

func joinErr(a, b error) error {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return errors.Join(a, b)
}
