package main

import (
  "context"
  "encoding/json"
  "errors"
  "fmt"
  "log"
  "net/http"
  "os"
  "strconv"
  "strings"
  "sync"
  "time"

  "github.com/Ap3pp3rs94/Chartly2.0/services/topics/internal/discovery"
  "github.com/Ap3pp3rs94/Chartly2.0/services/topics/internal/fetcher"
  "github.com/Ap3pp3rs94/Chartly2.0/services/topics/internal/insights"
  "github.com/Ap3pp3rs94/Chartly2.0/services/topics/internal/normalize"
  "github.com/Ap3pp3rs94/Chartly2.0/services/topics/internal/store"
  "github.com/Ap3pp3rs94/Chartly2.0/services/topics/internal/types"
  "gopkg.in/yaml.v3"
)

type Config struct {
  Server struct {
    Port int `yaml:"port"`
  } `yaml:"server"`
  Endpoints struct {
    ControlPlaneURL string `yaml:"control_plane_url"`
    ResultsPath     string `yaml:"results_path"`
  } `yaml:"endpoints"`
  Topics struct {
    Root string `yaml:"root"`
  } `yaml:"topics"`
  Schedule struct {
    RawFetchInterval    string `yaml:"raw_fetch_interval"`
    CodexProcessInterval string `yaml:"codex_process_interval"`
  } `yaml:"schedule"`
  Limits struct {
    SampleLimit       int `yaml:"sample_limit"`
    PerTopicMaxSources int `yaml:"per_topic_max_sources"`
    MaxRawBytes       int `yaml:"max_raw_bytes"`
  } `yaml:"limits"`
  Codex struct {
    ExecutorURL  string `yaml:"executor_url"`
    ExecutorPath string `yaml:"executor_path"`
    MaxTokens    int    `yaml:"max_tokens"`
  } `yaml:"codex"`
}

type App struct {
  cfg Config
  st *store.Store
  lastTickAt time.Time
  lastOkAt time.Time
  lastErr string
  mu sync.RWMutex
}

func main() {
  cfgPath := "/app/config/topics.yaml"
  if len(os.Args) >= 3 && os.Args[1] == "--config" {
    cfgPath = os.Args[2]
  }

  cfg, err := loadConfig(cfgPath)
  if err != nil {
    log.Fatalf("config: %v", err)
  }
  applyEnv(&cfg)

  if cfg.Server.Port == 0 { cfg.Server.Port = 8087 }
  if cfg.Endpoints.ControlPlaneURL == "" { cfg.Endpoints.ControlPlaneURL = "http://gateway:8090" }
  if cfg.Endpoints.ResultsPath == "" { cfg.Endpoints.ResultsPath = "/api/results" }
  if cfg.Topics.Root == "" { cfg.Topics.Root = "/app/profiles/topics" }
  if cfg.Schedule.RawFetchInterval == "" { cfg.Schedule.RawFetchInterval = "5m" }
  if cfg.Schedule.CodexProcessInterval == "" { cfg.Schedule.CodexProcessInterval = "10m" }
  if cfg.Limits.SampleLimit == 0 { cfg.Limits.SampleLimit = 50 }
  if cfg.Limits.PerTopicMaxSources == 0 { cfg.Limits.PerTopicMaxSources = 20 }
  if cfg.Limits.MaxRawBytes == 0 { cfg.Limits.MaxRawBytes = 1048576 }
  if cfg.Codex.ExecutorPath == "" { cfg.Codex.ExecutorPath = "/execute" }
  if cfg.Codex.MaxTokens == 0 { cfg.Codex.MaxTokens = 2200 }

  st := store.New()
  topics, err := loadTopics(cfg.Topics.Root)
  if err != nil {
    log.Printf("topic load error: %v", err)
  }
  st.UpsertTopics(topics)

  app := &App{cfg: cfg, st: st}

  // loops
  go app.rawLoop()
  go app.codexLoop()

  mux := http.NewServeMux()
  mux.HandleFunc("/health", app.handleHealth)
  mux.HandleFunc("/api/topics", app.handleTopics)
  mux.HandleFunc("/api/topics/", app.handleTopicActions)
  mux.HandleFunc("/api/tick", app.handleTick)

  addr := ":" + strconv.Itoa(cfg.Server.Port)
  srv := &http.Server{Addr: addr, Handler: withCORS(withLogger(mux))}
  log.Printf("topics service listening on %s", addr)
  if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
    log.Fatalf("server: %v", err)
  }
}

func loadConfig(path string) (Config, error) {
  var cfg Config
  b, err := os.ReadFile(path)
  if err != nil {
    return cfg, err
  }
  if err := yaml.Unmarshal(b, &cfg); err != nil {
    return cfg, err
  }
  return cfg, nil
}

func applyEnv(cfg *Config) {
  if v := os.Getenv("PORT"); v != "" {
    if p, err := strconv.Atoi(v); err == nil { cfg.Server.Port = p }
  }
  if v := os.Getenv("CONTROL_PLANE_URL"); v != "" { cfg.Endpoints.ControlPlaneURL = v }
  if v := os.Getenv("RESULTS_PATH"); v != "" { cfg.Endpoints.ResultsPath = v }
  if v := os.Getenv("TOPICS_ROOT"); v != "" { cfg.Topics.Root = v }
  if v := os.Getenv("RAW_FETCH_INTERVAL"); v != "" { cfg.Schedule.RawFetchInterval = v }
  if v := os.Getenv("CODEX_PROCESS_INTERVAL"); v != "" { cfg.Schedule.CodexProcessInterval = v }
  if v := os.Getenv("SAMPLE_LIMIT"); v != "" { if n, err := strconv.Atoi(v); err == nil { cfg.Limits.SampleLimit = n } }
  if v := os.Getenv("PER_TOPIC_MAX_SOURCES"); v != "" { if n, err := strconv.Atoi(v); err == nil { cfg.Limits.PerTopicMaxSources = n } }
  if v := os.Getenv("MAX_RAW_BYTES"); v != "" { if n, err := strconv.Atoi(v); err == nil { cfg.Limits.MaxRawBytes = n } }
  if v := os.Getenv("CODEX_EXECUTOR_URL"); v != "" { cfg.Codex.ExecutorURL = v }
  if v := os.Getenv("CODEX_EXECUTOR_PATH"); v != "" { cfg.Codex.ExecutorPath = v }
  if v := os.Getenv("CODEX_MAX_TOKENS"); v != "" { if n, err := strconv.Atoi(v); err == nil { cfg.Codex.MaxTokens = n } }
}

func loadTopics(root string) ([]*types.TopicProfile, error) {
  entries, err := os.ReadDir(root)
  if err != nil {
    return nil, err
  }
  out := []*types.TopicProfile{}
  for _, e := range entries {
    if e.IsDir() { continue }
    name := strings.ToLower(e.Name())
    if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") { continue }
    b, err := os.ReadFile(root + string(os.PathSeparator) + e.Name())
    if err != nil { continue }
    var tp types.TopicProfile
    if err := yaml.Unmarshal(b, &tp); err != nil { continue }
    if tp.ID == "" { continue }
    out = append(out, &tp)
  }
  types.SortTopics(out)
  return out, nil
}

func (a *App) rawLoop() {
  interval := parseDur(a.cfg.Schedule.RawFetchInterval, 5*time.Minute)
  ticker := time.NewTicker(interval)
  defer ticker.Stop()
  a.runRawFetch(context.Background())
  for range ticker.C {
    a.runRawFetch(context.Background())
  }
}

func (a *App) codexLoop() {
  interval := parseDur(a.cfg.Schedule.CodexProcessInterval, 10*time.Minute)
  ticker := time.NewTicker(interval)
  defer ticker.Stop()
  a.runCodex(context.Background())
  for range ticker.C {
    a.runCodex(context.Background())
  }
}

func (a *App) runRawFetch(ctx context.Context) {
  topics := a.st.ListTopics()
  fetcher.FetchAll(ctx, a.st, topics, fetcher.Options{
    PerSourceTimeout: parseDur(os.Getenv("PER_SOURCE_TIMEOUT"), 15*time.Second),
    SampleLimit: clamp(a.cfg.Limits.SampleLimit, 1, 200),
    MaxRawBytes: clamp(a.cfg.Limits.MaxRawBytes, 1, 5*1024*1024),
    PerTopicMaxSources: clamp(a.cfg.Limits.PerTopicMaxSources, 1, 50),
  })
  a.markOk()
}

func (a *App) runCodex(ctx context.Context) {
  if a.cfg.Codex.ExecutorURL == "" {
    return
  }
  topics := a.st.ListTopics()
  active := []*types.TopicProfile{}
  for _, t := range topics {
    if t.Active { active = append(active, t) }
  }
  if len(active) == 0 { return }

  outputs, err := normalize.ProcessBatch(ctx, normalize.Options{
    ExecutorURL: a.cfg.Codex.ExecutorURL,
    ExecutorPath: a.cfg.Codex.ExecutorPath,
    MaxTokens: a.cfg.Codex.MaxTokens,
  }, a.st, active)
  if err != nil {
    a.markErr(err.Error())
    return
  }

  // Post to control plane
  for _, out := range outputs {
    insights.PostResult(ctx, a.cfg.Endpoints.ControlPlaneURL, a.cfg.Endpoints.ResultsPath, out)
  }
  a.markOk()
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
  a.mu.RLock()
  resp := map[string]any{
    "status": "healthy",
    "topics_total": len(a.st.ListTopics()),
    "topics_active": a.st.CountActive(),
    "last_tick_at": a.lastTickAt.UTC().Format(time.RFC3339),
    "last_ok_at": a.lastOkAt.UTC().Format(time.RFC3339),
  }
  if a.lastErr != "" { resp["last_error"] = a.lastErr }
  a.mu.RUnlock()
  writeJSON(w, 200, resp)
}

func (a *App) handleTopics(w http.ResponseWriter, r *http.Request) {
  if r.Method == http.MethodGet {
    items := a.st.ListTopicSummaries()
    writeJSON(w, 200, map[string]any{"topics": items})
    return
  }
  if r.Method == http.MethodPost {
    var body struct{ ID, Name string }
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil { writeJSON(w, 400, map[string]any{"ok":false}); return }
    if body.ID == "" { writeJSON(w, 400, map[string]any{"ok":false, "message":"missing_id"}); return }
    tp := &types.TopicProfile{ID: body.ID, Name: body.Name, Version: "1.0.0"}
    // discover once
    sources := discovery.Discover(tp.ID)
    tp.Sources = sources
    a.st.UpsertTopics([]*types.TopicProfile{tp})
    writeJSON(w, 200, map[string]any{"ok":true, "topic": tp})
    return
  }
  writeJSON(w, 405, map[string]any{"ok":false})
}

func (a *App) handleTopicActions(w http.ResponseWriter, r *http.Request) {
  parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/topics/"), "/")
  if len(parts) < 2 { writeJSON(w,404,map[string]any{"ok":false}); return }
  id := parts[0]
  action := parts[1]

  switch action {
  case "activate":
    if r.Method != http.MethodPost { writeJSON(w,405,map[string]any{"ok":false}); return }
    var body struct{ Active bool }
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil { writeJSON(w,400,map[string]any{"ok":false}); return }
    a.st.SetActive(id, body.Active)
    writeJSON(w,200,map[string]any{"ok":true,"active":body.Active})
  case "discover":
    if r.Method != http.MethodPost { writeJSON(w,405,map[string]any{"ok":false}); return }
    var body struct{ Force bool }
    _ = json.NewDecoder(r.Body).Decode(&body)
    if t := a.st.GetTopic(id); t != nil {
      t.Sources = discovery.Discover(id)
      a.st.UpsertTopics([]*types.TopicProfile{t})
      writeJSON(w,200,map[string]any{"ok":true})
    } else {
      writeJSON(w,404,map[string]any{"ok":false})
    }
  default:
    writeJSON(w,404,map[string]any{"ok":false})
  }
}

func (a *App) handleTick(w http.ResponseWriter, r *http.Request) {
  if r.Method != http.MethodPost { writeJSON(w,405,map[string]any{"ok":false}); return }
  if admin := os.Getenv("ADMIN_KEY"); admin != "" {
    if r.Header.Get("X-API-Key") != admin { writeJSON(w,403,map[string]any{"ok":false}); return }
  }
  go a.runRawFetch(context.Background())
  go a.runCodex(context.Background())
  writeJSON(w, 200, map[string]any{"ok":true})
}

func (a *App) markOk() {
  a.mu.Lock()
  a.lastOkAt = time.Now()
  a.lastTickAt = time.Now()
  a.lastErr = ""
  a.mu.Unlock()
}

func (a *App) markErr(msg string) {
  a.mu.Lock()
  a.lastTickAt = time.Now()
  a.lastErr = msg
  a.mu.Unlock()
}

func parseDur(v string, def time.Duration) time.Duration {
  if v == "" { return def }
  d, err := time.ParseDuration(v)
  if err != nil { return def }
  return d
}

func clamp(n, min, max int) int {
  if n < min { return min }
  if n > max { return max }
  return n
}

func writeJSON(w http.ResponseWriter, status int, v any) {
  w.Header().Set("Content-Type", "application/json")
  w.WriteHeader(status)
  _ = json.NewEncoder(w).Encode(v)
}

func withCORS(next http.Handler) http.Handler {
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")
    w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
    if r.Method == http.MethodOptions {
      w.WriteHeader(http.StatusNoContent)
      return
    }
    next.ServeHTTP(w, r)
  })
}

func withLogger(next http.Handler) http.Handler {
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    start := time.Now()
    rw := &respWriter{ResponseWriter: w, status: 200}
    next.ServeHTTP(rw, r)
    logLine := map[string]any{
      "ts": time.Now().UTC().Format(time.RFC3339),
      "level": "info",
      "method": r.Method,
      "path": r.URL.Path,
      "status": rw.status,
      "duration_ms": time.Since(start).Milliseconds(),
    }
    b, _ := json.Marshal(logLine)
    fmt.Println(string(b))
  })
}

type respWriter struct {
  http.ResponseWriter
  status int
}
func (r *respWriter) WriteHeader(code int) {
  r.status = code
  r.ResponseWriter.WriteHeader(code)
}


