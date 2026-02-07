package main

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultPort = "8080"

	defaultRegistryURL     = "http://registry:8081"
	defaultAggregatorURL   = "http://aggregator:8082"
	defaultCoordinatorURL  = "http://coordinator:8083"
	defaultReporterURL     = "http://reporter:8084"
	defaultAnalyticsURL    = "http://analytics:8086"
	defaultCryptoStreamURL = "http://crypto-stream:8088"

	defaultRateLimitRPS   = 10
	defaultRateLimitBurst = 20

	distDir = "/app/web/dist"
)

type serviceDetail struct {
	Status     string `json:"status"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Error      string `json:"error,omitempty"`
}

type statusDetailed struct {
	Status   string                   `json:"status"`
	Services map[string]serviceDetail `json:"services"`
}

type reportSpec struct {
	Profiles []string `json:"profiles"`
	JoinKey  string   `json:"join_key"`
	Metrics  []string `json:"metrics"`
	Mode     string   `json:"mode"`
}

type reportEntry struct {
	ID        string
	CreatedAt time.Time
	Spec      reportSpec
}

type reportStore struct {
	mu    sync.Mutex
	items map[string]reportEntry
	order []string
}

func newReportStore() *reportStore {
	return &reportStore{items: make(map[string]reportEntry)}
}

func (s *reportStore) add(spec reportSpec) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := fmt.Sprintf("report-%d", time.Now().UnixNano())
	s.items[id] = reportEntry{ID: id, CreatedAt: time.Now().UTC(), Spec: spec}
	s.order = append(s.order, id)
	if len(s.order) > 100 {
		toDrop := s.order[:len(s.order)-100]
		for _, rid := range toDrop {
			delete(s.items, rid)
		}
		s.order = s.order[len(s.order)-100:]
	}
	return id
}

func (s *reportStore) list() []reportEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]reportEntry, 0, len(s.order))
	for _, id := range s.order {
		if it, ok := s.items[id]; ok {
			out = append(out, it)
		}
	}
	return out
}

func (s *reportStore) get(id string) (reportEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[id]
	return it, ok
}

type ctxKey string

const (
	ctxPrincipal ctxKey = "principal"
	ctxTenant    ctxKey = "tenant"
)

func main() {
	registryURL := envOr("REGISTRY_URL", defaultRegistryURL)
	aggregatorURL := envOr("AGGREGATOR_URL", defaultAggregatorURL)
	coordinatorURL := envOr("COORDINATOR_URL", defaultCoordinatorURL)
	reporterURL := envOr("REPORTER_URL", defaultReporterURL)
	analyticsURL := envOr("ANALYTICS_URL", defaultAnalyticsURL)
	cryptoStreamURL := envOr("CRYPTO_STREAM_URL", defaultCryptoStreamURL)

	regProxy := mustProxy(registryURL)
	aggProxy := mustProxy(aggregatorURL)
	cooProxy := mustProxy(coordinatorURL)
	repProxy := mustProxy(reporterURL)
	anaProxy := mustProxy(analyticsURL)

	reports := newReportStore()

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
			return
		}

		sum := checkAll(registryURL, aggregatorURL, coordinatorURL, reporterURL, analyticsURL)
		status := "healthy"
		if sum["services"].(map[string]string)["registry"] != "up" ||
			sum["services"].(map[string]string)["aggregator"] != "up" ||
			sum["services"].(map[string]string)["coordinator"] != "up" ||
			sum["services"].(map[string]string)["reporter"] != "up" ||
			sum["services"].(map[string]string)["analytics"] != "up" {
			status = "degraded"
		}
		sum["status"] = status
		writeJSON(w, http.StatusOK, sum)
	})

	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
			return
		}
		writeJSON(w, http.StatusOK, metricsSnapshot())
	})

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
			return
		}

		out := checkAllDetailed(registryURL, aggregatorURL, coordinatorURL, reporterURL, analyticsURL)
		status := "healthy"
		for _, v := range out.Services {
			if v.Status != "up" {
				status = "degraded"
				break
			}
		}
		out.Status = status
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "stream_not_supported"})
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		logLine("INFO", "sse_connect", "path=%s", r.URL.Path)

		ctx := r.Context()
		heartbeatTicker := time.NewTicker(2 * time.Second)
		tickTicker := time.NewTicker(5 * time.Second)
		defer heartbeatTicker.Stop()
		defer tickTicker.Stop()

		writeSSE := func(event string, payload any) {
			b, _ := json.Marshal(payload)
			fmt.Fprintf(w, "event: %s\n", event)
			fmt.Fprintf(w, "data: %s\n\n", string(b))
			flusher.Flush()
		}

		for {
			select {
			case <-ctx.Done():
				logLine("INFO", "sse_disconnect", "path=%s", r.URL.Path)
				return
			case <-heartbeatTicker.C:
				svc := checkAll(registryURL, aggregatorURL, coordinatorURL, reporterURL, analyticsURL)
				payload := map[string]any{
					"status":   "ok",
					"ts":       time.Now().UTC().Format(time.RFC3339),
					"services": svc["services"],
				}
				writeSSE("heartbeat", payload)
			case <-tickTicker.C:
				writeSSE("tick", map[string]any{"ts": time.Now().UTC().Format(time.RFC3339)})
			}
		}
	})

	// Proxies (strip /api prefix)
	mux.Handle("/api/profiles/", stripPrefixProxy("/api", regProxy))
	mux.Handle("/api/profiles", stripPrefixProxy("/api", regProxy))

	mux.Handle("/api/results/", stripPrefixProxy("/api", aggProxy))
	mux.Handle("/api/results", stripPrefixProxy("/api", aggProxy))

	mux.Handle("/api/runs/", stripPrefixProxy("/api", aggProxy))
	mux.Handle("/api/runs", stripPrefixProxy("/api", aggProxy))

	mux.Handle("/api/records/", stripPrefixProxy("/api", aggProxy))
	mux.Handle("/api/records", stripPrefixProxy("/api", aggProxy))

	mux.Handle("/api/drones/", stripPrefixProxy("/api", cooProxy))
	mux.Handle("/api/drones", stripPrefixProxy("/api", cooProxy))

	mux.HandleFunc("/api/reports", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		switch r.Method {
		case http.MethodGet:
			base := []map[string]any{
				{"id": "live-crypto-wall", "name": "Live Crypto Wall", "type": "live_grid", "refresh_ms": 2000},
				{"id": "crypto-index", "name": "Crypto Index", "type": "timeseries", "refresh_ms": 2000},
			}
			for _, it := range reports.list() {
				base = append(base, map[string]any{
					"id":         it.ID,
					"name":       "Custom Report",
					"type":       "correlation",
					"refresh_ms": 2000,
				})
			}
			writeJSON(w, http.StatusOK, base)
		case http.MethodPost:
			var spec reportSpec
			if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
				return
			}
			id := reports.add(spec)
			writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "created"})
		default:
			repProxy.ServeHTTP(w, r)
		}
	})

	mux.HandleFunc("/api/reports/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			repProxy.ServeHTTP(w, r)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/reports/")
		if id == "" || strings.Contains(id, "/") {
			repProxy.ServeHTTP(w, r)
			return
		}
		switch id {
		case "live-crypto-wall":
			payload, err := buildLiveCryptoWall(r.Context(), aggregatorURL)
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]any{"error": "upstream_error"})
				return
			}
			writeJSON(w, http.StatusOK, payload)
			return
		case "crypto-index":
			payload, err := buildCryptoIndex(r.Context(), aggregatorURL)
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]any{"error": "upstream_error"})
				return
			}
			writeJSON(w, http.StatusOK, payload)
			return
		default:
			if _, ok := reports.get(id); ok {
				payload, err := buildCryptoIndex(r.Context(), aggregatorURL)
				if err != nil {
					writeJSON(w, http.StatusBadGateway, map[string]any{"error": "upstream_error"})
					return
				}
				payload["id"] = id
				payload["title"] = "Custom Report"
				writeJSON(w, http.StatusOK, payload)
				return
			}
		}
		repProxy.ServeHTTP(w, r)
	})

	mux.HandleFunc("/api/crypto/symbols", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
			return
		}
		target := strings.TrimSuffix(cryptoStreamURL, "/") + "/symbols"
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
		c := &http.Client{Timeout: 5 * time.Second}
		resp, err := c.Do(req)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "upstream_error", "upstream": "crypto-stream", "status": 0})
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "upstream_error", "upstream": "crypto-stream", "status": resp.StatusCode})
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, resp.Body)
	})

	// Analytics service expects /api/analytics/* paths (do not strip /api)
	mux.Handle("/api/analytics/", anaProxy)
	mux.Handle("/api/analytics", anaProxy)

	// Static + SPA fallback (everything else)
	mux.HandleFunc("/", serveSPA(distDir))

	authCfg := loadAuthConfig()
	rateLimiter := newRateLimiter(
		envInt("RATE_LIMIT_RPS", defaultRateLimitRPS),
		envInt("RATE_LIMIT_BURST", defaultRateLimitBurst),
	)

	// Middleware order: X-Request-ID -> Logging -> CORS -> Auth -> RateLimit
	var handler http.Handler = mux
	handler = withRateLimit(rateLimiter)(handler)
	handler = withAuth(authCfg)(handler)
	handler = withCORS(handler)
	handler = withLogging(handler)
	handler = withRequestID(handler)

	addr := ":" + defaultPort
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	logLine("INFO", "starting", "addr=%s registry=%s aggregator=%s coordinator=%s reporter=%s analytics=%s crypto=%s", addr, registryURL, aggregatorURL, coordinatorURL, reporterURL, analyticsURL, cryptoStreamURL)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logLine("ERROR", "listen_failed", "err=%s", err.Error())
		os.Exit(1)
	}
}

func envOr(k, def string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	return v
}

func envInt(k string, def int) int {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	if n, err := strconvAtoiSafe(v); err == nil {
		return n
	}
	return def
}

func mustProxy(target string) *httputil.ReverseProxy {
	u, err := url.Parse(target)
	if err != nil {
		panic(err)
	}
	p := httputil.NewSingleHostReverseProxy(u)
	orig := p.Director
	p.Director = func(r *http.Request) {
		orig(r)
		if rid := r.Header.Get("X-Request-ID"); rid != "" {
			r.Header.Set("X-Request-ID", rid)
		}
		if principal := principalFromContext(r.Context()); principal != "" {
			r.Header.Set("X-Principal", principal)
		}
		if tenant := tenantFromContext(r.Context()); tenant != "" {
			r.Header.Set("X-Tenant-ID", tenant)
		}
	}
	p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "upstream_unavailable"})
	}
	return p
}

func stripPrefixProxy(prefix string, proxy *httputil.ReverseProxy) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
		if r.URL.Path == "" {
			r.URL.Path = "/"
		}
		proxy.ServeHTTP(w, r)
	})
}

func serveSPA(root string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/status" || r.URL.Path == "/health" {
			http.NotFound(w, r)
			return
		}

		p := r.URL.Path
		if p == "" || p == "/" {
			p = "/index.html"
		}
		clean := filepath.Clean(p)
		full := filepath.Join(root, filepath.FromSlash(clean))

		if fi, err := os.Stat(full); err == nil && !fi.IsDir() {
			http.ServeFile(w, r, full)
			return
		}

		index := filepath.Join(root, "index.html")
		if _, err := os.Stat(index); err == nil {
			http.ServeFile(w, r, index)
			return
		}

		http.NotFound(w, r)
	}
}

func checkAll(reg, agg, coo, rep, ana string) map[string]any {
	svcs := map[string]string{
		"registry":    upOrDown(reg + "/health"),
		"aggregator":  upOrDown(agg + "/health"),
		"coordinator": upOrDown(coo + "/health"),
		"reporter":    upOrDown(rep + "/health"),
		"analytics":   upOrDown(ana + "/health"),
	}
	return map[string]any{
		"status":   "healthy",
		"services": svcs,
	}
}

func checkAllDetailed(reg, agg, coo, rep, ana string) statusDetailed {
	return statusDetailed{
		Status: "healthy",
		Services: map[string]serviceDetail{
			"registry":    upOrDownDetailed(reg + "/health"),
			"aggregator":  upOrDownDetailed(agg + "/health"),
			"coordinator": upOrDownDetailed(coo + "/health"),
			"reporter":    upOrDownDetailed(rep + "/health"),
			"analytics":   upOrDownDetailed(ana + "/health"),
		},
	}
}

func upOrDown(url string) string {
	d := upOrDownDetailed(url)
	return d.Status
}

func upOrDownDetailed(hurl string) serviceDetail {
	c := &http.Client{Timeout: 2 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, hurl, nil)
	resp, err := c.Do(req)
	if err != nil {
		return serviceDetail{Status: "down", Error: "request_failed"}
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return serviceDetail{Status: "down", HTTPStatus: resp.StatusCode, Error: "non_2xx"}
	}
	return serviceDetail{Status: "up", HTTPStatus: resp.StatusCode}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

type aggResult struct {
	ProfileID string    `json:"profile_id"`
	Timestamp string    `json:"timestamp"`
	Data      any       `json:"data"`
	CreatedAt time.Time `json:"created_at"`
}

func fetchAggregatorResults(ctx context.Context, aggURL, profileID string, limit int) ([]aggResult, error) {
	u := fmt.Sprintf("%s/results?profile_id=%s&limit=%d", strings.TrimSuffix(aggURL, "/"), url.QueryEscape(profileID), limit)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	c := &http.Client{Timeout: 6 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("non_2xx: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out []aggResult
	if err := json.Unmarshal(body, &out); err == nil {
		return out, nil
	}
	// fallback: generic decode
	var generic []map[string]any
	if err := json.Unmarshal(body, &generic); err != nil {
		return nil, err
	}
	for _, row := range generic {
		ar := aggResult{}
		if v, ok := row["profile_id"].(string); ok {
			ar.ProfileID = v
		}
		if v, ok := row["timestamp"].(string); ok {
			ar.Timestamp = v
		}
		if v, ok := row["data"]; ok {
			ar.Data = v
		}
		out = append(out, ar)
	}
	return out, nil
}

func parseTimeRFC3339(s string) (time.Time, bool) {
	if strings.TrimSpace(s) == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func asMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	}
	return ""
}

func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	case string:
		if t == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(t, 64)
		return f, err == nil
	}
	return 0, false
}

func resultData(row aggResult) map[string]any {
	if m := asMap(row.Data); m != nil {
		return m
	}
	return nil
}

func getSymbol(data map[string]any) string {
	if data == nil {
		return ""
	}
	if s := asString(data["symbol"]); s != "" {
		return s
	}
	if s := asString(data["s"]); s != "" {
		return s
	}
	if raw := asMap(data["raw"]); raw != nil {
		if s := asString(raw["s"]); s != "" {
			return s
		}
	}
	return ""
}

func getTimestamp(row aggResult, data map[string]any) time.Time {
	if row.Timestamp != "" {
		if t, ok := parseTimeRFC3339(row.Timestamp); ok {
			return t
		}
	}
	if ts := asString(data["timestamp"]); ts != "" {
		if t, ok := parseTimeRFC3339(ts); ok {
			return t
		}
	}
	return time.Now().UTC()
}

func buildLiveCryptoWall(ctx context.Context, aggURL string) (map[string]any, error) {
	rows, err := fetchAggregatorResults(ctx, aggURL, "crypto-watchlist", 500)
	if err != nil {
		return nil, err
	}
	type rowOut struct {
		Symbol    string  `json:"symbol"`
		Price     float64 `json:"price"`
		PctChange float64 `json:"pct_change"`
		Volume    float64 `json:"volume"`
		QuoteVol  float64 `json:"quote_volume"`
		High      float64 `json:"high"`
		Low       float64 `json:"low"`
		Open      float64 `json:"open"`
		Updated   string  `json:"updated"`
	}
	latest := make(map[string]rowOut)
	for _, r := range rows {
		data := resultData(r)
		if data == nil {
			continue
		}
		symbol := getSymbol(data)
		if symbol == "" {
			continue
		}
		ts := getTimestamp(r, data)
		price, _ := asFloat(data["c"])
		if price == 0 {
			price, _ = asFloat(data["price"])
		}
		pct, _ := asFloat(data["pct_change"])
		vol, _ := asFloat(data["v"])
		qv, _ := asFloat(data["q"])
		high, _ := asFloat(data["h"])
		low, _ := asFloat(data["l"])
		open, _ := asFloat(data["o"])
		latest[symbol] = rowOut{
			Symbol:    symbol,
			Price:     price,
			PctChange: pct,
			Volume:    vol,
			QuoteVol:  qv,
			High:      high,
			Low:       low,
			Open:      open,
			Updated:   ts.Format(time.RFC3339),
		}
	}
	rowsOut := make([]rowOut, 0, len(latest))
	for _, v := range latest {
		rowsOut = append(rowsOut, v)
	}
	sort.Slice(rowsOut, func(i, j int) bool { return rowsOut[i].Symbol < rowsOut[j].Symbol })
	return map[string]any{
		"id":         "live-crypto-wall",
		"title":      "Live Crypto Wall",
		"updated_at": time.Now().UTC().Format(time.RFC3339),
		"rows":       rowsOut,
		"series":     []any{},
		"meta": map[string]any{
			"source_profiles": []string{"crypto-watchlist"},
			"window":          "last_30m",
		},
	}, nil
}

func buildCryptoIndex(ctx context.Context, aggURL string) (map[string]any, error) {
	rows, err := fetchAggregatorResults(ctx, aggURL, "crypto-watchlist", 500)
	if err != nil {
		return nil, err
	}
	type point struct {
		T string  `json:"t"`
		Y float64 `json:"y"`
	}
	points := make([]point, 0, 500)
	for _, r := range rows {
		data := resultData(r)
		if data == nil {
			continue
		}
		if getSymbol(data) != "CRYPTO_INDEX_USDT" {
			continue
		}
		ts := getTimestamp(r, data)
		val, ok := asFloat(data["c"])
		if !ok {
			continue
		}
		points = append(points, point{T: ts.Format(time.RFC3339), Y: val})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].T < points[j].T })
	return map[string]any{
		"id":         "crypto-index",
		"title":      "Crypto Index",
		"updated_at": time.Now().UTC().Format(time.RFC3339),
		"series": []any{
			map[string]any{
				"name":   "CRYPTO_INDEX_USDT",
				"points": points,
			},
		},
		"meta": map[string]any{
			"source_profiles": []string{"crypto-watchlist"},
			"window":          "last_30m",
		},
	}, nil
}

// --- Auth + Rate limiting ---

type authConfig struct {
	Enabled          bool
	Issuer           string
	Audience         []string
	JWKSURL          string
	HS256Secret      string
	HS256SecretFile  string
	LeewaySeconds    int64
	APIKeys          map[string]struct{}
	APIKeysFile      string
	APIKeysTTL       time.Duration
	AllowAnonymous   map[string]struct{}
	JWKSCacheTTL     time.Duration
	RequireAuthPaths []string
	JWKS             *jwksCache
	RequireTenant    bool
	TenantClaim      string
	TenantHeader     string
}

func loadAuthConfig() *authConfig {
	issuer := strings.TrimSpace(os.Getenv("AUTH_JWT_ISSUER"))
	jwksURL := strings.TrimSpace(os.Getenv("AUTH_JWT_JWKS_URL"))
	hsecret := strings.TrimSpace(os.Getenv("AUTH_JWT_HS256_SECRET"))
	hsecretFile := strings.TrimSpace(os.Getenv("AUTH_JWT_HS256_SECRET_FILE"))
	aud := strings.TrimSpace(os.Getenv("AUTH_JWT_AUDIENCE"))
	leeway := envInt64("AUTH_JWT_LEEWAY_SECONDS", 60)
	cacheTTL := time.Duration(envInt64("AUTH_JWT_JWKS_TTL_SECONDS", 600)) * time.Second
	apiKeysTTL := time.Duration(envInt64("AUTH_API_KEYS_TTL_SECONDS", 60)) * time.Second
	requireTenant := envBool("AUTH_TENANT_REQUIRED", false)
	tenantClaim := strings.TrimSpace(os.Getenv("AUTH_TENANT_CLAIM"))
	if tenantClaim == "" {
		tenantClaim = "tenant_id"
	}
	tenantHeader := strings.TrimSpace(os.Getenv("AUTH_TENANT_HEADER"))
	if tenantHeader == "" {
		tenantHeader = "X-Tenant-ID"
	}

	apiKeysFile := strings.TrimSpace(os.Getenv("AUTH_API_KEYS_FILE"))
	apiKeys := parseKeySet(os.Getenv("AUTH_API_KEYS"))
	if hsecret == "" && hsecretFile != "" {
		hsecret = strings.TrimSpace(readFileString(hsecretFile))
	}

	cfg := &authConfig{
		Issuer:          issuer,
		JWKSURL:         jwksURL,
		HS256Secret:     hsecret,
		HS256SecretFile: hsecretFile,
		LeewaySeconds:   leeway,
		Audience:        splitCSV(aud),
		APIKeys:         apiKeys,
		APIKeysFile:     apiKeysFile,
		APIKeysTTL:      apiKeysTTL,
		AllowAnonymous: map[string]struct{}{
			"/health":             {},
			"/api/status":         {},
			"/api/events":         {},
			"/api/reports":        {},
			"/api/crypto/symbols": {},
			"/metrics":            {},
			"/favicon.ico":        {},
		},
		JWKSCacheTTL:  cacheTTL,
		RequireTenant: requireTenant,
		TenantClaim:   tenantClaim,
		TenantHeader:  tenantHeader,
	}

	cfg.Enabled = cfg.Issuer != "" || cfg.JWKSURL != "" || cfg.HS256Secret != "" || len(cfg.APIKeys) > 0
	if cfg.JWKSURL != "" {
		cfg.JWKS = newJWKSCache(cfg.JWKSURL, cacheTTL)
	}
	return cfg
}

func withAuth(cfg *authConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if !cfg.Enabled {
				next.ServeHTTP(w, r)
				return
			}
			if _, ok := cfg.AllowAnonymous[r.URL.Path]; ok || strings.HasPrefix(r.URL.Path, "/api/reports/") {
				next.ServeHTTP(w, r)
				return
			}

			principal, tenant, ok := authenticateRequest(cfg, r)
			if !ok {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
				return
			}
			if cfg.RequireTenant && tenant == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "tenant_required"})
				return
			}

			ctx := context.WithValue(r.Context(), ctxPrincipal, principal)
			ctx = context.WithValue(ctx, ctxTenant, tenant)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func authenticateRequest(cfg *authConfig, r *http.Request) (string, string, bool) {
	tenantHeader := strings.TrimSpace(r.Header.Get(cfg.TenantHeader))
	if key := strings.TrimSpace(r.Header.Get("X-API-Key")); key != "" {
		if apiKeyValid(cfg, key) {
			tenant := ""
			if cfg.RequireTenant {
				tenant = tenantHeader
			}
			return "apikey:" + shortKeyHash(key), tenant, true
		}
	}
	if authz := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		tok := strings.TrimSpace(authz[len("bearer "):])
		claims, err := validateJWT(cfg, tok)
		if err == nil {
			tenant := tenantFromClaims(cfg, claims)
			if tenantHeader != "" && tenant != "" && tenantHeader != tenant {
				return "", "", false
			}
			if sub, _ := claims["sub"].(string); sub != "" {
				return "jwt:" + sub, tenant, true
			}
			return "jwt:anonymous", tenant, true
		}
	}
	return "", "", false
}

func apiKeyValid(cfg *authConfig, key string) bool {
	keySet := cfg.APIKeys
	if cfg.APIKeysFile != "" {
		keySet = getAPIKeysFromFile(cfg.APIKeysFile, cfg.APIKeysTTL)
	}
	if len(keySet) == 0 {
		return false
	}
	h := sha256Hex([]byte(key))
	_, ok := keySet[h]
	return ok
}

// --- JWT ---

type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

type jwksCache struct {
	mu      sync.RWMutex
	url     string
	ttl     time.Duration
	lastRef time.Time
	keys    map[string]*rsa.PublicKey
	client  *http.Client
}

type jwksDoc struct {
	Keys []struct {
		Kty string `json:"kty"`
		Kid string `json:"kid"`
		N   string `json:"n"`
		E   string `json:"e"`
		Alg string `json:"alg"`
	} `json:"keys"`
}

func newJWKSCache(url string, ttl time.Duration) *jwksCache {
	return &jwksCache{
		url:    url,
		ttl:    ttl,
		keys:   make(map[string]*rsa.PublicKey),
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *jwksCache) getKey(kid string) (*rsa.PublicKey, error) {
	c.mu.RLock()
	k := c.keys[kid]
	fresh := time.Since(c.lastRef) < c.ttl
	c.mu.RUnlock()
	if k != nil && fresh {
		return k, nil
	}
	if err := c.refresh(); err != nil {
		return nil, err
	}
	c.mu.RLock()
	k = c.keys[kid]
	c.mu.RUnlock()
	if k == nil {
		return nil, errors.New("jwks_key_not_found")
	}
	return k, nil
}

func (c *jwksCache) refresh() error {
	resp, err := c.client.Get(c.url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return errors.New("jwks_fetch_failed")
	}
	var doc jwksDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return err
	}
	keys := make(map[string]*rsa.PublicKey)
	for _, k := range doc.Keys {
		if strings.ToUpper(k.Kty) != "RSA" {
			continue
		}
		pub, err := jwkToPublicKey(k.N, k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	c.mu.Lock()
	c.keys = keys
	c.lastRef = time.Now()
	c.mu.Unlock()
	return nil
}

func jwkToPublicKey(n, e string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(n)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(e)
	if err != nil {
		return nil, err
	}
	var eInt int
	for _, b := range eBytes {
		eInt = eInt<<8 + int(b)
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: eInt}, nil
}

func validateJWT(cfg *authConfig, token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid_token")
	}

	hBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("invalid_header")
	}
	var hdr jwtHeader
	if err := json.Unmarshal(hBytes, &hdr); err != nil {
		return nil, errors.New("invalid_header")
	}

	pBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("invalid_payload")
	}
	var claims map[string]any
	if err := json.Unmarshal(pBytes, &claims); err != nil {
		return nil, errors.New("invalid_payload")
	}

	if !validateClaims(cfg, claims) {
		return nil, errors.New("invalid_claims")
	}

	signed := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, errors.New("invalid_signature")
	}

	alg := strings.ToUpper(hdr.Alg)
	switch alg {
	case "RS256":
		if cfg.JWKS == nil {
			return nil, errors.New("jwks_not_configured")
		}
		pub, err := cfg.JWKS.getKey(hdr.Kid)
		if err != nil {
			return nil, err
		}
		hash := sha256.Sum256([]byte(signed))
		if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, hash[:], sig); err != nil {
			return nil, errors.New("invalid_signature")
		}
	case "HS256":
		if cfg.HS256Secret == "" {
			return nil, errors.New("hs256_not_configured")
		}
		mac := hmac.New(sha256.New, []byte(cfg.HS256Secret))
		mac.Write([]byte(signed))
		expected := mac.Sum(nil)
		if subtle.ConstantTimeCompare(expected, sig) != 1 {
			return nil, errors.New("invalid_signature")
		}
	default:
		return nil, errors.New("unsupported_alg")
	}

	return claims, nil
}

func validateClaims(cfg *authConfig, claims map[string]any) bool {
	if cfg.Issuer != "" {
		if iss, _ := claims["iss"].(string); iss != cfg.Issuer {
			return false
		}
	}
	if len(cfg.Audience) > 0 {
		if !audMatches(cfg.Audience, claims["aud"]) {
			return false
		}
	}
	now := time.Now().Unix()
	leeway := cfg.LeewaySeconds
	if exp, ok := numClaim(claims, "exp"); ok {
		if now > exp+leeway {
			return false
		}
	}
	if nbf, ok := numClaim(claims, "nbf"); ok {
		if now < nbf-leeway {
			return false
		}
	}
	return true
}

func audMatches(allowed []string, aud any) bool {
	switch v := aud.(type) {
	case string:
		for _, a := range allowed {
			if v == a {
				return true
			}
		}
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok {
				for _, a := range allowed {
					if s == a {
						return true
					}
				}
			}
		}
	}
	return false
}

func numClaim(claims map[string]any, key string) (int64, bool) {
	v, ok := claims[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return int64(t), true
	case int64:
		return t, true
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return n, true
		}
	}
	return 0, false
}

// --- Rate limiter ---

type rateLimiter struct {
	rps   int
	burst int
	mu    sync.Mutex
	bkt   map[string]*tokenBucket
}

type tokenBucket struct {
	last   time.Time
	tokens float64
	burst  float64
	ratePS float64
}

func newRateLimiter(rps, burst int) *rateLimiter {
	if rps < 1 {
		rps = defaultRateLimitRPS
	}
	if burst < 1 {
		burst = defaultRateLimitBurst
	}
	return &rateLimiter{rps: rps, burst: burst, bkt: make(map[string]*tokenBucket)}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b, ok := rl.bkt[key]
	if !ok {
		b = &tokenBucket{last: time.Now(), tokens: float64(rl.burst), burst: float64(rl.burst), ratePS: float64(rl.rps)}
		rl.bkt[key] = b
	}
	now := time.Now()
	delta := now.Sub(b.last).Seconds()
	b.tokens = minf(b.burst, b.tokens+delta*b.ratePS)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens -= 1
	return true
}

func withRateLimit(rl *rateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			key := rateKey(r)
			if !rl.allow(key) {
				writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "rate_limited"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func rateKey(r *http.Request) string {
	if p := principalFromContext(r.Context()); p != "" {
		if t := tenantFromContext(r.Context()); t != "" {
			return p + "@tenant:" + t
		}
		return p
	}
	if xf := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xf != "" {
		parts := strings.Split(xf, ",")
		return "ip:" + strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return "ip:" + host
	}
	return "ip:" + r.RemoteAddr
}

// --- Middleware ---

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if rid == "" {
			rid = mustUUIDv4()
			r.Header.Set("X-Request-ID", rid)
		}
		w.Header().Set("X-Request-ID", rid)
		next.ServeHTTP(w, r)
	})
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		dur := time.Since(start).Milliseconds()
		ts := time.Now().UTC().Format(time.RFC3339)
		rid := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		metricsRecord(rec.status, dur)
		fmt.Fprintf(os.Stdout, "%s method=%s path=%s status=%d duration_ms=%d request_id=%s\n",
			ts, r.Method, r.URL.Path, rec.status, dur, rid)
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID, X-API-Key, Authorization, X-Tenant-ID")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func mustUUIDv4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	s := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", s[0:8], s[8:12], s[12:16], s[16:20], s[20:32])
}

func logLine(level, msg, format string, args ...any) {
	ts := time.Now().UTC().Format(time.RFC3339)
	line := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stdout, "%s %s %s %s\n", ts, level, msg, line)
}

// --- helpers ---

func splitCSV(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func parseKeySet(v string) map[string]struct{} {
	keys := splitCSV(v)
	if len(keys) == 0 {
		return map[string]struct{}{}
	}
	out := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		h := sha256Hex([]byte(k))
		out[h] = struct{}{}
	}
	return out
}

type apiKeyFileCache struct {
	mu      sync.RWMutex
	path    string
	ttl     time.Duration
	last    time.Time
	modTime time.Time
	keys    map[string]struct{}
}

var apiKeyCache = &apiKeyFileCache{}

func getAPIKeysFromFile(path string, ttl time.Duration) map[string]struct{} {
	if path == "" {
		return map[string]struct{}{}
	}
	apiKeyCache.mu.Lock()
	defer apiKeyCache.mu.Unlock()

	if apiKeyCache.path != path {
		apiKeyCache.path = path
		apiKeyCache.keys = nil
		apiKeyCache.last = time.Time{}
		apiKeyCache.modTime = time.Time{}
	}

	if time.Since(apiKeyCache.last) < ttl && apiKeyCache.keys != nil {
		return apiKeyCache.keys
	}

	fi, err := os.Stat(path)
	if err != nil {
		apiKeyCache.keys = map[string]struct{}{}
		apiKeyCache.last = time.Now()
		return apiKeyCache.keys
	}
	if apiKeyCache.modTime.Equal(fi.ModTime()) && apiKeyCache.keys != nil {
		apiKeyCache.last = time.Now()
		return apiKeyCache.keys
	}

	b, err := os.ReadFile(path)
	if err != nil {
		apiKeyCache.keys = map[string]struct{}{}
		apiKeyCache.last = time.Now()
		return apiKeyCache.keys
	}
	lines := strings.Split(string(b), "\n")
	keys := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		h := sha256Hex([]byte(s))
		keys[h] = struct{}{}
	}
	apiKeyCache.keys = keys
	apiKeyCache.last = time.Now()
	apiKeyCache.modTime = fi.ModTime()
	return keys
}

func readFileString(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func shortKeyHash(k string) string {
	h := sha256Hex([]byte(k))
	if len(h) < 8 {
		return h
	}
	return h[:8]
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func principalFromContext(ctx context.Context) string {
	if v := ctx.Value(ctxPrincipal); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func tenantFromContext(ctx context.Context) string {
	if v := ctx.Value(ctxTenant); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func envInt64(k string, def int64) int64 {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	if n, err := strconvParseInt(v); err == nil {
		return n
	}
	return def
}

func strconvAtoiSafe(s string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(s))
}

func strconvParseInt(s string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
}

func envBool(k string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return def
	}
}

func tenantFromClaims(cfg *authConfig, claims map[string]any) string {
	if cfg.TenantClaim == "" {
		return ""
	}
	if v, ok := claims[cfg.TenantClaim]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// --- minimal metrics ---

var metricsMu sync.Mutex
var metricsReq int64
var metricsErr int64
var metricsDurMs int64

func metricsRecord(status int, durMs int64) {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	metricsReq++
	if status >= 400 {
		metricsErr++
	}
	metricsDurMs += durMs
}

func metricsSnapshot() map[string]any {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	avg := int64(0)
	if metricsReq > 0 {
		avg = metricsDurMs / metricsReq
	}
	return map[string]any{
		"requests_total":   metricsReq,
		"errors_total":     metricsErr,
		"avg_duration_ms":  avg,
		"last_updated_utc": time.Now().UTC().Format(time.RFC3339),
	}
}

// ACCEPTANCE TESTS:
// curl http://localhost:8090/api/events
// curl http://localhost:8090/api/reports
// curl http://localhost:8090/api/reports/crypto-index
// curl http://localhost:8090/api/crypto/symbols
