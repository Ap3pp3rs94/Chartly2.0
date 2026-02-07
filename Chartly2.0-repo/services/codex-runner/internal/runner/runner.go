package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Port               string
	RunnerID           string
	ProfileRoot        string
	ProfileAllowlist   []string
	RunInterval        time.Duration
	SampleLimit        int
	OutputLimit        int
	OutputMaxBytes     int
	FetchTimeout       time.Duration
	ExecTimeout        time.Duration
	HTTPRetries        int
	HTTPBackoffBase    time.Duration
	ControlPlaneURL    string
	ResultsPath        string
	CodexExecutorURL   string
	CodexExecutorPath  string
	RunnerAdminKey     string
	AllowExternalFetch bool
}

type Runner struct {
	cfg Config

	mu           sync.RWMutex
	profiles     []Profile
	lastTickAt   time.Time
	lastOkAt     time.Time
	lastError    string
	ready        bool
	stopped      bool
	recentRuns   *RunRing
	metrics      *Metrics
	rateLimiter  map[string]time.Time
	client       *http.Client
	execClient   *http.Client
	ticker       *time.Ticker
	tickCancel   context.CancelFunc
}

type runSummary struct {
	ProfileID  string   `json:"profile_id"`
	StartedAt  string   `json:"started_at"`
	DurationMs int64    `json:"duration_ms"`
	RecordsIn  int      `json:"records_in"`
	RecordsOut int      `json:"records_out"`
	InputHash  string   `json:"input_hash"`
	OutputHash string   `json:"output_hash"`
	Status     string   `json:"status"`
	Missing    []string `json:"missing_vars"`
	Error      string   `json:"error_message"`
}

func LoadConfig() Config {
	cfg := loadConfigFile()

	if cfg.Port == "" {
		cfg.Port = "8086"
	}
	if cfg.RunnerID == "" {
		cfg.RunnerID = "codex-runner"
	}
	if cfg.ProfileRoot == "" {
		cfg.ProfileRoot = "/app/profiles/government"
	}
	if cfg.RunInterval == 0 {
		cfg.RunInterval = 5 * time.Minute
	}
	if cfg.SampleLimit == 0 {
		cfg.SampleLimit = 50
	}
	if cfg.OutputLimit == 0 {
		cfg.OutputLimit = 1000
	}
	if cfg.OutputMaxBytes == 0 {
		cfg.OutputMaxBytes = 1048576
	}
	if cfg.FetchTimeout == 0 {
		cfg.FetchTimeout = 30 * time.Second
	}
	if cfg.ExecTimeout == 0 {
		cfg.ExecTimeout = 60 * time.Second
	}
	if cfg.HTTPRetries == 0 {
		cfg.HTTPRetries = 3
	}
	if cfg.HTTPBackoffBase == 0 {
		cfg.HTTPBackoffBase = 1 * time.Second
	}
	if cfg.ControlPlaneURL == "" {
		cfg.ControlPlaneURL = "http://gateway:8090"
	}
	if cfg.ResultsPath == "" {
		cfg.ResultsPath = "/api/results"
	}
	if cfg.CodexExecutorPath == "" {
		cfg.CodexExecutorPath = "/execute"
	}

	// Env overrides
	cfg.Port = getEnv("PORT", cfg.Port)
	cfg.RunnerID = getEnv("RUNNER_ID", cfg.RunnerID)
	cfg.ProfileRoot = getEnv("PROFILE_ROOT", cfg.ProfileRoot)
	cfg.RunInterval = getEnvDur("RUN_INTERVAL", cfg.RunInterval)
	cfg.SampleLimit = clampInt(getEnvInt("SAMPLE_LIMIT", cfg.SampleLimit), 1, 200)
	cfg.OutputLimit = clampInt(getEnvInt("OUTPUT_LIMIT", cfg.OutputLimit), 1, 5000)
	cfg.OutputMaxBytes = clampInt(getEnvInt("OUTPUT_MAX_BYTES", cfg.OutputMaxBytes), 1, 5242880)
	cfg.FetchTimeout = getEnvDur("FETCH_TIMEOUT", cfg.FetchTimeout)
	cfg.ExecTimeout = getEnvDur("EXEC_TIMEOUT", cfg.ExecTimeout)
	cfg.HTTPRetries = clampInt(getEnvInt("HTTP_RETRIES", cfg.HTTPRetries), 0, 10)
	cfg.HTTPBackoffBase = getEnvDur("HTTP_BACKOFF_BASE", cfg.HTTPBackoffBase)
	cfg.ControlPlaneURL = getEnv("CONTROL_PLANE_URL", cfg.ControlPlaneURL)
	cfg.ResultsPath = getEnv("RESULTS_PATH", cfg.ResultsPath)
	cfg.CodexExecutorURL = strings.TrimSpace(getEnv("CODEX_EXECUTOR_URL", cfg.CodexExecutorURL))
	cfg.CodexExecutorPath = getEnv("CODEX_EXECUTOR_PATH", cfg.CodexExecutorPath)
	cfg.RunnerAdminKey = strings.TrimSpace(getEnv("RUNNER_ADMIN_KEY", cfg.RunnerAdminKey))
	cfg.AllowExternalFetch = getEnvBool("ALLOW_EXTERNAL_FETCH", cfg.AllowExternalFetch)

	if v := strings.TrimSpace(os.Getenv("PROFILE_ALLOWLIST")); v != "" {
		parts := strings.Split(v, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				cfg.ProfileAllowlist = append(cfg.ProfileAllowlist, p)
			}
		}
	}
	return cfg
}

func loadConfigFile() Config {
	paths := []string{
		"/app/config/codex-runner.yaml",
		filepath.Join("services", "codex-runner", "config", "codex-runner.yaml"),
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var raw struct {
			Server struct {
				Port int `yaml:"port"`
			} `yaml:"server"`
			Runner struct {
				RunInterval    string `yaml:"run_interval"`
				SampleLimit    int    `yaml:"sample_limit"`
				OutputLimit    int    `yaml:"output_limit"`
				OutputMaxBytes int    `yaml:"output_max_bytes"`
			} `yaml:"runner"`
			Endpoints struct {
				ControlPlaneURL   string `yaml:"control_plane_url"`
				ResultsPath       string `yaml:"results_path"`
				CodexExecutorURL  string `yaml:"codex_executor_url"`
				CodexExecutorPath string `yaml:"codex_executor_path"`
			} `yaml:"endpoints"`
			Profiles struct {
				Root string `yaml:"root"`
			} `yaml:"profiles"`
		}
		dec := yaml.NewDecoder(bytes.NewReader(b))
		dec.KnownFields(false)
		if err := dec.Decode(&raw); err != nil {
			continue
		}
		cfg := Config{}
		if raw.Server.Port != 0 {
			cfg.Port = strconv.Itoa(raw.Server.Port)
		}
		if raw.Runner.RunInterval != "" {
			if d, err := time.ParseDuration(raw.Runner.RunInterval); err == nil {
				cfg.RunInterval = d
			}
		}
		cfg.SampleLimit = raw.Runner.SampleLimit
		cfg.OutputLimit = raw.Runner.OutputLimit
		cfg.OutputMaxBytes = raw.Runner.OutputMaxBytes
		cfg.ControlPlaneURL = raw.Endpoints.ControlPlaneURL
		cfg.ResultsPath = raw.Endpoints.ResultsPath
		cfg.CodexExecutorURL = raw.Endpoints.CodexExecutorURL
		cfg.CodexExecutorPath = raw.Endpoints.CodexExecutorPath
		cfg.ProfileRoot = raw.Profiles.Root
		return cfg
	}
	return Config{}
}

func NewRunner(cfg Config) *Runner {
	return &Runner{
		cfg:         cfg,
		recentRuns:  NewRunRing(200),
		metrics:     NewMetrics(),
		rateLimiter: make(map[string]time.Time),
		client:      &http.Client{Timeout: cfg.FetchTimeout},
		execClient:  &http.Client{Timeout: cfg.ExecTimeout},
	}
}

func (r *Runner) Start() {
	r.tickOnce(context.Background())
	r.ready = true
	r.ticker = time.NewTicker(r.cfg.RunInterval)
	ctx, cancel := context.WithCancel(context.Background())
	r.tickCancel = cancel
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-r.ticker.C:
				r.tickOnce(ctx)
			}
		}
	}()
}

func (r *Runner) Stop() {
	r.mu.Lock()
	r.stopped = true
	r.mu.Unlock()
	if r.tickCancel != nil {
		r.tickCancel()
	}
	if r.ticker != nil {
		r.ticker.Stop()
	}
}

func (r *Runner) tickOnce(ctx context.Context) {
	start := time.Now()
	profiles, err := LoadProfiles(r.cfg.ProfileRoot, r.cfg.ProfileAllowlist)
	if err != nil {
		r.setError("profiles_load_failed")
		return
	}
	if len(profiles) == 0 {
		r.setTickOk()
		r.lastTickAt = time.Now().UTC()
		return
	}
	r.mu.Lock()
	r.profiles = profiles
	r.mu.Unlock()

	for _, p := range profiles {
		r.runProfile(ctx, p)
	}
	r.metrics.RecordDuration(time.Since(start))
	r.setTickOk()
}

func (r *Runner) setTickOk() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastTickAt = time.Now().UTC()
	r.lastOkAt = r.lastTickAt
	r.lastError = ""
}

func (r *Runner) setError(msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastTickAt = time.Now().UTC()
	r.lastError = msg
}

func (r *Runner) runProfile(ctx context.Context, p Profile) {
	start := time.Now()
	summary := runSummary{ProfileID: p.ID, StartedAt: time.Now().UTC().Format(time.RFC3339)}

	missingVars := []string{}
	if !r.cfg.AllowExternalFetch {
		summary.Status = "skipped"
		summary.Error = "external_fetch_disabled"
		r.recentRuns.Add(summary)
		r.logEvent("INFO", p.ID, "skipped", time.Since(start), 0, 0, "external_fetch_disabled")
		return
	}

	if !r.rateLimitAllow(p) {
		summary.Status = "skipped"
		summary.Error = "rate_limited"
		r.recentRuns.Add(summary)
		r.logEvent("INFO", p.ID, "skipped", time.Since(start), 0, 0, "rate_limited")
		return
	}

	fetchReq, ferr := BuildFetchRequest(p)
	if ferr != nil {
		summary.Status = "failed"
		summary.Error = "invalid_profile"
		r.recentRuns.Add(summary)
		r.logEvent("ERROR", p.ID, "fetch", time.Since(start), 0, 0, "invalid_profile")
		return
	}

	respBody, missing, ferr := FetchSample(r.client, fetchReq, r.cfg.SampleLimit, r.cfg.HTTPRetries, r.cfg.HTTPBackoffBase)
	missingVars = append(missingVars, missing...)
	if ferr != nil {
		summary.Status = "failed"
		summary.Error = "fetch_failed"
		r.recentRuns.Add(summary)
		r.logEvent("ERROR", p.ID, "fetch", time.Since(start), 0, 0, "fetch_failed")
		return
	}

	sampleRecords := respBody
	summary.RecordsIn = len(sampleRecords)
	inputHash := hashCanonical(sampleRecords)
	summary.InputHash = inputHash

	prompt := BuildPrompt(p, sampleRecords, r.cfg.OutputLimit, r.cfg.OutputMaxBytes)

	outStr, err := CallExecutor(r.execClient, r.cfg, p.ID, prompt, r.cfg.ExecTimeout)
	if err != nil {
		summary.Status = "failed"
		summary.Error = "executor_failed"
		r.recentRuns.Add(summary)
		r.logEvent("ERROR", p.ID, "exec", time.Since(start), summary.RecordsIn, 0, "executor_failed")
		return
	}

	validated, outHash, err := ValidateOutput(outStr, r.cfg.OutputLimit, r.cfg.OutputMaxBytes)
	if err != nil {
		summary.Status = "failed"
		summary.Error = "validate_failed"
		r.recentRuns.Add(summary)
		r.logEvent("ERROR", p.ID, "validate", time.Since(start), summary.RecordsIn, 0, "validate_failed")
		return
	}

	summary.RecordsOut = len(validated)
	summary.OutputHash = outHash

	err = PostResults(r.client, r.cfg, p, validated, inputHash, outHash, missingVars, r.cfg.HTTPRetries, r.cfg.HTTPBackoffBase)
	if err != nil {
		summary.Status = "failed"
		summary.Error = "post_failed"
		r.recentRuns.Add(summary)
		r.logEvent("ERROR", p.ID, "post", time.Since(start), summary.RecordsIn, summary.RecordsOut, "post_failed")
		return
	}

	summary.Status = "ok"
	summary.Missing = missingVars
	summary.DurationMs = time.Since(start).Milliseconds()
	r.recentRuns.Add(summary)
	r.logEvent("INFO", p.ID, "ok", time.Since(start), summary.RecordsIn, summary.RecordsOut, "")
}

func (r *Runner) rateLimitAllow(p Profile) bool {
	limit := p.RateLimit
	if limit <= 0 {
		return true
	}
	minGap := time.Minute / time.Duration(limit)
	now := time.Now()
	last, ok := r.rateLimiter[p.ID]
	if ok && now.Sub(last) < minGap {
		return false
	}
	r.rateLimiter[p.ID] = now
	return true
}

// --- HTTP handlers ---

func (r *Runner) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "healthy",
		"runner_id":       r.cfg.RunnerID,
		"profiles_loaded": len(r.profiles),
		"last_tick_at":    r.lastTickAt.Format(time.RFC3339),
		"last_ok_at":      r.lastOkAt.Format(time.RFC3339),
		"last_error":      r.lastError,
	})
}

func (r *Runner) HandleReady(w http.ResponseWriter, _ *http.Request) {
	if r.ready {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
		return
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready"})
}

func (r *Runner) HandleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(r.metrics.Snapshot()))
}

func (r *Runner) HandleRunsRecent(w http.ResponseWriter, req *http.Request) {
	limit := clampInt(getQueryInt(req, "limit", 50), 1, 200)
	writeJSON(w, http.StatusOK, r.recentRuns.List(limit))
}

func (r *Runner) HandleRunsTrigger(w http.ResponseWriter, req *http.Request) {
	if r.cfg.RunnerAdminKey != "" {
		if req.Header.Get("X-API-Key") != r.cfg.RunnerAdminKey {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
			return
		}
	}
	go r.tickOnce(context.Background())
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

func (r *Runner) logEvent(level, profileID, phase string, dur time.Duration, in, out int, errMsg string) {
	line := map[string]any{
		"ts":          time.Now().UTC().Format(time.RFC3339),
		"level":       level,
		"msg":         "run",
		"runner_id":   r.cfg.RunnerID,
		"profile_id":  profileID,
		"phase":       phase,
		"duration_ms": dur.Milliseconds(),
		"records_in":  in,
		"records_out": out,
		"err":         errMsg,
	}
	b, _ := json.Marshal(line)
	fmt.Fprintln(os.Stdout, string(b))
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func hashCanonical(v any) string {
	b, _ := canonicalJSON(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func canonicalJSON(v any) ([]byte, error) {
	buf := &bytes.Buffer{}
	if err := writeCanonical(buf, v); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

func writeCanonical(buf *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if t { buf.WriteString("true") } else { buf.WriteString("false") }
	case string:
		b, _ := json.Marshal(t)
		buf.Write(b)
	case float64, float32, int, int64, int32, int16, int8, uint, uint64, uint32, uint16, uint8:
		b, _ := json.Marshal(t)
		buf.Write(b)
	case []any:
		buf.WriteByte('[')
		for i, it := range t {
			if i > 0 { buf.WriteByte(',') }
			if err := writeCanonical(buf, it); err != nil { return err }
		}
		buf.WriteByte(']')
	case map[string]any:
		buf.WriteByte('{')
		keys := make([]string, 0, len(t))
		for k := range t { keys = append(keys, k) }
		sort.Strings(keys)
		for i, k := range keys {
			if i > 0 { buf.WriteByte(',') }
			kb, _ := json.Marshal(k)
			buf.Write(kb)
			buf.WriteByte(':')
			if err := writeCanonical(buf, t[k]); err != nil { return err }
		}
		buf.WriteByte('}')
	default:
		b, err := json.Marshal(t)
		if err != nil { return err }
		buf.Write(b)
	}
	return nil
}

func getEnv(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

func getEnvInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

func getEnvDur(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func getEnvBool(key string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return def
	}
}

func clampInt(v, min, max int) int {
	if v < min { return min }
	if v > max { return max }
	return v
}

func getQueryInt(r *http.Request, key string, def int) int {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" { return def }
	i, err := strconv.Atoi(v)
	if err != nil { return def }
	return i
}

// --- run ring + metrics ---

type RunRing struct {
	mu   sync.Mutex
	cap  int
	list []runSummary
}

func NewRunRing(capacity int) *RunRing {
	return &RunRing{cap: capacity}
}

func (r *RunRing) Add(s runSummary) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.list) >= r.cap {
		r.list = r.list[1:]
	}
	r.list = append(r.list, s)
}

func (r *RunRing) List(limit int) []runSummary {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit > len(r.list) { limit = len(r.list) }
	out := make([]runSummary, 0, limit)
	for i := len(r.list) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, r.list[i])
	}
	return out
}

type Metrics struct {
	mu       sync.Mutex
	count    int64
	failures int64
	durMs    int64
}

func NewMetrics() *Metrics { return &Metrics{} }

func (m *Metrics) RecordDuration(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.count++
	m.durMs += d.Milliseconds()
}

func (m *Metrics) Snapshot() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	avg := int64(0)
	if m.count > 0 { avg = m.durMs / m.count }
	return fmt.Sprintf("runs_total %d\nrun_failures %d\navg_duration_ms %d\n", m.count, m.failures, avg)
}

var errMissingExecutor = errors.New("missing_executor_url")

func CallExecutor(client *http.Client, cfg Config, profileID, prompt string, timeout time.Duration) (string, error) {
	if strings.TrimSpace(cfg.CodexExecutorURL) == "" {
		return "", errMissingExecutor
	}
	body := map[string]any{
		"runner_id":  cfg.RunnerID,
		"profile_id": profileID,
		"temperature": 0,
		"max_tokens": 1800,
		"timeout_ms": int(timeout.Milliseconds()),
		"prompt":     prompt,
	}
	b, _ := json.Marshal(body)
	url := strings.TrimRight(cfg.CodexExecutorURL, "/") + cfg.CodexExecutorPath
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil { return "", err }
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 { return "", fmt.Errorf("executor_status_%d", resp.StatusCode) }
	var out struct {
		Ok     bool   `json:"ok"`
		Output string `json:"output"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil { return "", err }
	if !out.Ok || out.Output == "" { return "", errors.New("executor_invalid_output") }
	return out.Output, nil
}

func PostResults(client *http.Client, cfg Config, p Profile, records []map[string]any, inputHash, outputHash string, missing []string, retries int, backoff time.Duration) error {
	payload := map[string]any{
		"drone_id":   cfg.RunnerID,
		"profile_id": p.ID,
		"data": map[string]any{
			"records": records,
			"meta": map[string]any{
				"profile_version": p.Version,
				"input_hash":      inputHash,
				"output_hash":     outputHash,
				"missing_vars":    missing,
			},
		},
	}
	b, _ := json.Marshal(payload)
	url := strings.TrimRight(cfg.ControlPlaneURL, "/") + cfg.ResultsPath
	return DoWithRetry(retries, backoff, func() error {
		req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil { return err }
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 { return fmt.Errorf("post_status_%d", resp.StatusCode) }
		return nil
	})
}
