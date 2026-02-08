package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"gopkg.in/yaml.v3"
)

type config struct {
	AggregatorURL  string
	RegistryURL    string
	BinanceWSURL   string
	BinanceRESTURL string
	ProfileID      string
	DroneID        string
	RunID          string
	BatchMax       int
	FlushInterval  time.Duration
	PostTimeout    time.Duration
	RESTTimeout    time.Duration
	ReconnectDelay time.Duration
	RESTPollEvery  time.Duration
	HealthAddr     string
	ProjectMode    string
	ProjectMinVol  float64
	WatchlistID    string
	WatchlistEvery time.Duration
	Watchlist      *watchlistState
}

type resultIn struct {
	DroneID   string            `json:"drone_id"`
	ProfileID string            `json:"profile_id"`
	RunID     string            `json:"run_id"`
	Data      []json.RawMessage `json:"data"`
}

type runIn struct {
	RunID      string `json:"run_id"`
	DroneID    string `json:"drone_id"`
	ProfileID  string `json:"profile_id"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	Status     string `json:"status"`
	RowsOut    int64  `json:"rows_out"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error"`
}

func main() {
	cfg := loadConfig()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go serveHealth(cfg.HealthAddr)
	if cfg.Watchlist != nil && cfg.WatchlistID != "" {
		go watchlistLoop(ctx, cfg, cfg.Watchlist)
	}

	startedAt := time.Now().UTC()
	rowsOut := int64(0)
	lastErr := atomic.Value{}
	wsUp := uint32(0)

	_ = postRun(ctx, cfg, runIn{
		RunID:     cfg.RunID,
		DroneID:   cfg.DroneID,
		ProfileID: cfg.ProfileID,
		StartedAt: startedAt.Format(time.RFC3339Nano),
		Status:    "running",
	})

	recordsCh := make(chan json.RawMessage, cfg.BatchMax*2)

	go func() {
		ticker := time.NewTicker(cfg.FlushInterval)
		defer ticker.Stop()

		buf := make([]json.RawMessage, 0, cfg.BatchMax)
		flush := func() {
			if len(buf) == 0 {
				return
			}
			if err := postResults(ctx, cfg, buf); err != nil {
				lastErr.Store(err)
				log.Printf("post_results error: %v", err)
			}
			buf = buf[:0]
		}

		for {
			select {
			case <-ctx.Done():
				flush()
				return
			case rec := <-recordsCh:
				buf = append(buf, rec)
				if len(buf) >= cfg.BatchMax {
					flush()
				}
			case <-ticker.C:
				flush()
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = postRun(ctx, cfg, runIn{
					RunID:      cfg.RunID,
					DroneID:    cfg.DroneID,
					ProfileID:  cfg.ProfileID,
					StartedAt:  startedAt.Format(time.RFC3339Nano),
					FinishedAt: "",
					Status:     "running",
					RowsOut:    atomic.LoadInt64(&rowsOut),
					DurationMs: time.Since(startedAt).Milliseconds(),
				})
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(cfg.RESTPollEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if atomic.LoadUint32(&wsUp) == 1 {
					continue
				}
				if err := pollREST(ctx, cfg, &rowsOut, recordsCh); err != nil {
					lastErr.Store(err)
					log.Printf("rest poll error: %v", err)
				}
			}
		}
	}()

	for {
		if err := runWS(ctx, cfg, &rowsOut, recordsCh, &wsUp); err != nil {
			lastErr.Store(err)
			log.Printf("ws loop error: %v", err)
		}

		select {
		case <-ctx.Done():
			goto shutdown
		default:
		}

		time.Sleep(cfg.ReconnectDelay)
	}

shutdown:
	finishedAt := time.Now().UTC()
	errMsg := ""
	if v := lastErr.Load(); v != nil {
		errMsg = v.(error).Error()
	}
	status := "completed"
	if errMsg != "" {
		status = "failed"
	}
	_ = postRun(context.Background(), cfg, runIn{
		RunID:      cfg.RunID,
		DroneID:    cfg.DroneID,
		ProfileID:  cfg.ProfileID,
		StartedAt:  startedAt.Format(time.RFC3339Nano),
		FinishedAt: finishedAt.Format(time.RFC3339Nano),
		Status:     status,
		RowsOut:    atomic.LoadInt64(&rowsOut),
		DurationMs: finishedAt.Sub(startedAt).Milliseconds(),
		Error:      errMsg,
	})
}

func runWS(ctx context.Context, cfg config, rowsOut *int64, recordsCh chan<- json.RawMessage, wsUp *uint32) error {
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(cfg.BinanceWSURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	atomic.StoreUint32(wsUp, 1)
	defer atomic.StoreUint32(wsUp, 0)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		records, err := normalizeMiniTicker(msg, cfg)
		if err != nil {
			continue
		}
		for _, r := range records {
			select {
			case recordsCh <- r:
				atomic.AddInt64(rowsOut, 1)
			default:
				// drop on backpressure to avoid unbounded memory
			}
		}
	}
}

type miniTicker struct {
	Symbol      string
	Close       float64
	Open        float64
	High        float64
	Low         float64
	Volume      float64
	QuoteVolume float64
	Raw         map[string]any
}

func pollREST(ctx context.Context, cfg config, rowsOut *int64, recordsCh chan<- json.RawMessage) error {
	reqCtx, cancel := context.WithTimeout(ctx, cfg.RESTTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, cfg.BinanceRESTURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: cfg.RESTTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("rest status %d", resp.StatusCode)
	}
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return err
	}
	records, err := normalizeMiniTicker(raw, cfg)
	if err != nil {
		return err
	}
	for _, r := range records {
		select {
		case recordsCh <- r:
			atomic.AddInt64(rowsOut, 1)
		default:
			// drop on backpressure to avoid unbounded memory
		}
	}
	return nil
}

func normalizeMiniTicker(raw []byte, cfg config) ([]json.RawMessage, error) {
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	out := make([]json.RawMessage, 0, len(arr)+1)
	parsed := make([]miniTicker, 0, len(arr))
	filter := cfg.Watchlist
	for _, item := range arr {
		item["source"] = "binance"
		item["event"] = "mini_ticker"
		item["ingest_ts"] = now
		if mt, ok := parseMini(item); ok {
			if filter != nil && !filter.allows(mt.Symbol) {
				continue
			}
			parsed = append(parsed, mt)
		}
		b, err := json.Marshal(item)
		if err != nil {
			continue
		}
		out = append(out, json.RawMessage(b))
	}
	if cfg.ProjectMode == "avg_usdt" {
		if proj, ok := projectIndexUSDT(parsed, cfg.ProjectMinVol, now); ok {
			out = append(out, proj)
		}
	}
	return out, nil
}

func parseMini(item map[string]any) (miniTicker, bool) {
	s, _ := item["s"].(string)
	if s == "" {
		return miniTicker{}, false
	}
	closeV, ok1 := parseFloat(item["c"])
	openV, ok2 := parseFloat(item["o"])
	highV, ok3 := parseFloat(item["h"])
	lowV, ok4 := parseFloat(item["l"])
	volV, ok5 := parseFloat(item["v"])
	qVolV, ok6 := parseFloat(item["q"])
	if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6) {
		return miniTicker{}, false
	}
	return miniTicker{
		Symbol:      s,
		Close:       closeV,
		Open:        openV,
		High:        highV,
		Low:         lowV,
		Volume:      volV,
		QuoteVolume: qVolV,
		Raw:         item,
	}, true
}

func projectIndexUSDT(items []miniTicker, minVol float64, now string) (json.RawMessage, bool) {
	var sumClose, sumOpen, sumHigh, sumLow float64
	var count int
	var totalVol, totalQVol float64
	for _, it := range items {
		if !strings.HasSuffix(it.Symbol, "USDT") {
			continue
		}
		if it.Volume < minVol {
			continue
		}
		sumClose += it.Close
		sumOpen += it.Open
		sumHigh += it.High
		sumLow += it.Low
		totalVol += it.Volume
		totalQVol += it.QuoteVolume
		count++
	}
	if count == 0 {
		return nil, false
	}
	out := map[string]any{
		"source":       "chartly",
		"event":        "projection_index",
		"symbol":       "CRYPTO_INDEX_USDT",
		"ingest_ts":    now,
		"constituents": count,
		"c":            fmt.Sprintf("%f", sumClose/float64(count)),
		"o":            fmt.Sprintf("%f", sumOpen/float64(count)),
		"h":            fmt.Sprintf("%f", sumHigh/float64(count)),
		"l":            fmt.Sprintf("%f", sumLow/float64(count)),
		"v":            fmt.Sprintf("%f", totalVol),
		"q":            fmt.Sprintf("%f", totalQVol),
		"projected":    true,
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, false
	}
	return json.RawMessage(b), true
}

func parseFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case string:
		f, err := strconv.ParseFloat(t, 64)
		return f, err == nil
	case float64:
		return t, true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func postResults(ctx context.Context, cfg config, data []json.RawMessage) error {
	payload := resultIn{
		DroneID:   cfg.DroneID,
		ProfileID: cfg.ProfileID,
		RunID:     cfg.RunID,
		Data:      data,
	}
	return postJSON(ctx, cfg, "/results", payload)
}

func postRun(ctx context.Context, cfg config, run runIn) error {
	return postJSON(ctx, cfg, "/runs", run)
}

func postJSON(ctx context.Context, cfg config, path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.AggregatorURL, "/")+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: cfg.PostTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("post %s failed: %s", path, resp.Status)
	}
	return nil
}

func serveHealth(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy"}`))
	})
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("health server error: %v", err)
	}
}

func loadConfig() config {
	wl := &watchlistState{}
	return config{
		AggregatorURL:  getenv("AGGREGATOR_URL", "http://aggregator:8082"),
		RegistryURL:    getenv("REGISTRY_URL", "http://registry:8081"),
		BinanceWSURL:   getenv("BINANCE_WS_URL", "wss://data-stream.binance.vision/ws/!miniTicker@arr"),
		BinanceRESTURL: getenv("BINANCE_REST_URL", "https://api.binance.com/api/v3/ticker/24hr"),
		ProfileID:      getenv("CRYPTO_PROFILE_ID", "crypto-watchlist"),
		DroneID:        getenv("CRYPTO_DRONE_ID", "crypto-binance-ws"),
		RunID:          getenv("CRYPTO_RUN_ID", "run_"+time.Now().UTC().Format("20060102T150405Z")),
		BatchMax:       getenvInt("CRYPTO_BATCH_MAX", 500),
		FlushInterval:  getenvDuration("CRYPTO_FLUSH_INTERVAL", 2*time.Second),
		PostTimeout:    getenvDuration("CRYPTO_POST_TIMEOUT", 10*time.Second),
		RESTTimeout:    getenvDuration("CRYPTO_REST_TIMEOUT", 8*time.Second),
		ReconnectDelay: getenvDuration("CRYPTO_RECONNECT_DELAY", 3*time.Second),
		RESTPollEvery:  getenvDuration("CRYPTO_REST_POLL_INTERVAL", 5*time.Second),
		HealthAddr:     getenv("CRYPTO_HEALTH_ADDR", ":8088"),
		ProjectMode:    getenv("CRYPTO_PROJECT_MODE", "avg_usdt"),
		ProjectMinVol:  getenvFloat("CRYPTO_PROJECT_MIN_VOL", 1000),
		WatchlistID:    getenv("CRYPTO_WATCHLIST_PROFILE_ID", "crypto-watchlist"),
		WatchlistEvery: getenvDuration("CRYPTO_WATCHLIST_REFRESH", 30*time.Second),
		Watchlist:      wl,
	}
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return def
	}
	return n
}

func getenvDuration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

func getenvFloat(key string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 {
		return def
	}
	return f
}

type watchlistState struct {
	value atomic.Value
}

func (w *watchlistState) set(symbols []string) {
	if len(symbols) == 0 {
		w.value.Store(map[string]struct{}{})
		return
	}
	m := make(map[string]struct{}, len(symbols))
	for _, s := range symbols {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		m[s] = struct{}{}
	}
	w.value.Store(m)
}

func (w *watchlistState) allows(symbol string) bool {
	v := w.value.Load()
	if v == nil {
		return true
	}
	m, ok := v.(map[string]struct{})
	if !ok || len(m) == 0 {
		return true
	}
	_, ok = m[strings.ToUpper(symbol)]
	return ok
}

type registryProfile struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Content string `json:"content"`
}

type watchlistDoc struct {
	Crypto struct {
		Symbols []string `yaml:"symbols"`
	} `yaml:"crypto"`
}

func watchlistLoop(ctx context.Context, cfg config, wl *watchlistState) {
	ticker := time.NewTicker(cfg.WatchlistEvery)
	defer ticker.Stop()

	refresh := func() {
		symbols, err := fetchWatchlist(ctx, cfg)
		if err != nil {
			log.Printf("watchlist refresh error: %v", err)
			return
		}
		wl.set(symbols)
	}

	refresh()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh()
		}
	}
}

func fetchWatchlist(ctx context.Context, cfg config) ([]string, error) {
	if cfg.RegistryURL == "" || cfg.WatchlistID == "" {
		return nil, nil
	}
	u := strings.TrimRight(cfg.RegistryURL, "/") + "/profiles/" + cfg.WatchlistID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("registry status %d", resp.StatusCode)
	}
	var prof registryProfile
	if err := json.NewDecoder(resp.Body).Decode(&prof); err != nil {
		return nil, err
	}
	var doc watchlistDoc
	if err := yaml.Unmarshal([]byte(prof.Content), &doc); err != nil {
		return nil, err
	}
	return doc.Crypto.Symbols, nil
}
