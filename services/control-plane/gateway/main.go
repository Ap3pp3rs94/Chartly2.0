package main

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
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

	"gopkg.in/yaml.v3"
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

//go:embed connectors_catalog.yaml
var connectorCatalogYAML []byte

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

type connectorCatalog struct {
	Version    string                  `yaml:"version"`
	Connectors []connectorCatalogEntry `yaml:"connectors"`
}

type connectorCatalogEntry struct {
	ID           string   `yaml:"id"`
	Name         string   `yaml:"name"`
	Kind         string   `yaml:"kind"`
	Enabled      bool     `yaml:"enabled"`
	Capabilities []string `yaml:"capabilities"`
	Endpoints    []struct {
		Notes string `yaml:"notes"`
	} `yaml:"endpoints"`
}

type connectorPublic struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	DisplayName  string   `json:"display_name"`
	Description  string   `json:"description,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type connectorConfigStore struct {
	mu    sync.Mutex
	items map[string]any
}

func newConnectorConfigStore() *connectorConfigStore {
	return &connectorConfigStore{items: make(map[string]any)}
}

func (s *connectorConfigStore) get(id string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.items[id]
	return v, ok
}

func (s *connectorConfigStore) set(id string, cfg any) {
	s.mu.Lock()
	s.items[id] = cfg
	s.mu.Unlock()
}

type auditEvent struct {
	EventID   string `json:"event_id"`
	EventTS   string `json:"event_ts"`
	Action    string `json:"action"`
	Outcome   string `json:"outcome"`
	ObjectKey string `json:"object_key"`
	RequestID string `json:"request_id,omitempty"`
	ActorID   string `json:"actor_id,omitempty"`
	Source    string `json:"source,omitempty"`
	Detail    any    `json:"detail_json,omitempty"`
}

type auditStore struct {
	mu     sync.Mutex
	events []auditEvent
	max    int
}

func newAuditStore(max int) *auditStore {
	if max <= 0 {
		max = 1000
	}
	return &auditStore{max: max}
}

func (s *auditStore) add(ev auditEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
	if len(s.events) > s.max {
		s.events = s.events[len(s.events)-s.max:]
	}
}

func (s *auditStore) list(limit int, since time.Time) []auditEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]auditEvent, 0, len(s.events))
	for _, ev := range s.events {
		if !since.IsZero() {
			ts, err := time.Parse(time.RFC3339, ev.EventTS)
			if err == nil && ts.Before(since) {
				continue
			}
		}
		out = append(out, ev)
	}
	if limit <= 0 || limit > len(out) {
		limit = len(out)
	}
	if limit < len(out) {
		out = out[len(out)-limit:]
	}
	return out
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

type summaryCache struct {
	mu      sync.Mutex
	expires time.Time
	data    map[string]any
}

type cryptoCache struct {
	mu          sync.RWMutex
	tickers     []binanceTicker
	lastUpdated time.Time
	lastErr     string
}

func (c *cryptoCache) set(ticks []binanceTicker, errMsg string) {
	c.mu.Lock()
	c.tickers = ticks
	c.lastErr = errMsg
	c.lastUpdated = time.Now().UTC()
	c.mu.Unlock()
}

func (c *cryptoCache) snapshot() ([]binanceTicker, time.Time, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cp := make([]binanceTicker, len(c.tickers))
	copy(cp, c.tickers)
	return cp, c.lastUpdated, c.lastErr
}

func (s *summaryCache) get() (map[string]any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil || time.Now().After(s.expires) {
		return nil, false
	}
	cp := make(map[string]any, len(s.data))
	for k, v := range s.data {
		cp[k] = v
	}
	return cp, true
}

func (s *summaryCache) set(data map[string]any, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = data
	s.expires = time.Now().Add(ttl)
}

type healthSnapshot struct {
	Status      string                   `json:"status"`
	Services    map[string]serviceDetail `json:"services"`
	LastSuccess map[string]string        `json:"last_success"`
	CheckedAt   string                   `json:"checked_at"`
}

type healthCache struct {
	mu          sync.Mutex
	lastSuccess map[string]time.Time
	snapshot    healthSnapshot
}

func newHealthCache() *healthCache {
	return &healthCache{lastSuccess: make(map[string]time.Time)}
}

func (h *healthCache) update(services map[string]serviceDetail) healthSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now().UTC()
	allUp := true
	for name, detail := range services {
		if detail.Status == "up" {
			h.lastSuccess[name] = now
		} else {
			allUp = false
		}
	}
	last := make(map[string]string, len(h.lastSuccess))
	for k, v := range h.lastSuccess {
		last[k] = v.Format(time.RFC3339)
	}
	status := "healthy"
	if !allUp {
		status = "degraded"
	}
	h.snapshot = healthSnapshot{
		Status:      status,
		Services:    services,
		LastSuccess: last,
		CheckedAt:   now.Format(time.RFC3339),
	}
	return h.snapshot
}

func (h *healthCache) get() healthSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snapshot
}

type sseEvent struct {
	ID    int64
	Event string
	Data  string
}

type sseHub struct {
	mu        sync.RWMutex
	nextID    int64
	buffer    []sseEvent
	maxBuffer int
	clients   map[chan sseEvent]struct{}
}

func newSSEHub(maxBuffer int) *sseHub {
	if maxBuffer < 1 {
		maxBuffer = 256
	}
	return &sseHub{
		maxBuffer: maxBuffer,
		clients:   make(map[chan sseEvent]struct{}),
	}
}

func (h *sseHub) publish(event string, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	h.mu.Lock()
	h.nextID++
	ev := sseEvent{ID: h.nextID, Event: event, Data: string(b)}
	h.buffer = append(h.buffer, ev)
	if len(h.buffer) > h.maxBuffer {
		h.buffer = h.buffer[len(h.buffer)-h.maxBuffer:]
	}
	for ch := range h.clients {
		select {
		case ch <- ev:
		default:
		}
	}
	h.mu.Unlock()
}

func (h *sseHub) addClient(ch chan sseEvent) {
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
}

func (h *sseHub) removeClient(ch chan sseEvent) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
}

func (h *sseHub) replaySince(id int64) []sseEvent {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if id <= 0 || len(h.buffer) == 0 {
		return nil
	}
	out := make([]sseEvent, 0, len(h.buffer))
	for _, ev := range h.buffer {
		if ev.ID > id {
			out = append(out, ev)
		}
	}
	return out
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
	health := newHealthCache()
	sse := newSSEHub(512)
	summary := &summaryCache{}
	crypto := &cryptoCache{}
	audit := newAuditStore(2000)
	connectors := newConnectorConfigStore()
	connCatalog := loadConnectorCatalog()
	connList := buildConnectorList(connCatalog)

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

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
			return
		}
		snap := health.get()
		if snap.CheckedAt == "" {
			services := checkAllDetailed(registryURL, aggregatorURL, coordinatorURL, reporterURL, analyticsURL).Services
			snap = health.update(services)
		}
		writeJSON(w, http.StatusOK, snap)
	})

	mux.HandleFunc("/api/gateway/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
			return
		}
		snap := health.get()
		if snap.CheckedAt == "" {
			services := checkAllDetailed(registryURL, aggregatorURL, coordinatorURL, reporterURL, analyticsURL).Services
			snap = health.update(services)
		}
		writeJSON(w, http.StatusOK, snap)
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

		rid := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		logLine("INFO", "sse_connect", "path=%s request_id=%s", r.URL.Path, rid)

		ctx := r.Context()
		lastID := parseLastEventID(r.Header.Get("Last-Event-ID"))
		ch := make(chan sseEvent, 16)
		sse.addClient(ch)
		defer sse.removeClient(ch)

		if lastID > 0 {
			for _, ev := range sse.replaySince(lastID) {
				writeSSEEvent(w, flusher, ev)
			}
		}

		// Immediate heartbeat on connect.
		services := checkAllDetailed(registryURL, aggregatorURL, coordinatorURL, reporterURL, analyticsURL).Services
		snap := health.update(services)
		writeSSEEvent(w, flusher, sseEvent{
			Event: "heartbeat",
			Data:  mustJSON(map[string]any{"status": snap.Status, "ts": time.Now().UTC().Format(time.RFC3339), "services": snapshotStatusMap(snap.Services)}),
		})

		keepalive := time.NewTicker(15 * time.Second)
		defer keepalive.Stop()

		for {
			select {
			case <-ctx.Done():
				logLine("INFO", "sse_disconnect", "path=%s request_id=%s", r.URL.Path, rid)
				return
			case ev := <-ch:
				writeSSEEvent(w, flusher, ev)
			case <-keepalive.C:
				fmt.Fprint(w, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	})

	resultsStreamHandler := func(w http.ResponseWriter, r *http.Request) {
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
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "streaming_not_supported"})
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		limit := clampInt(queryInt(r, "limit", 50), 1, 500)
		profileID := strings.TrimSpace(r.URL.Query().Get("profile_id"))
		pollMs := clampInt(queryInt(r, "poll_ms", 2000), 500, 10000)

		lastSeen := time.Time{}
		if since := strings.TrimSpace(r.URL.Query().Get("since")); since != "" {
			if t, ok := parseTimeRFC3339(since); ok {
				lastSeen = t
			}
		}
		seenIDs := make(map[string]struct{})

		send := func(event string, payload any) {
			b, _ := json.Marshal(payload)
			fmt.Fprintf(w, "event: %s\n", event)
			fmt.Fprintf(w, "data: %s\n\n", string(b))
			flusher.Flush()
		}

		ctx := r.Context()
		rid := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		logLine("INFO", "results_sse_connect", "path=%s request_id=%s", r.URL.Path, rid)

		if rows, err := fetchAggregatorResults(ctx, aggregatorURL, profileID, limit); err == nil {
			snapshot := make([]aggResult, 0, len(rows))
			snapshot = append(snapshot, rows...)
			if len(snapshot) > 0 {
				if ts := getTimestamp(snapshot[0], resultData(snapshot[0])); !ts.IsZero() {
					lastSeen = ts
					for _, row := range snapshot {
						if row.ID != "" {
							seenIDs[row.ID] = struct{}{}
						}
					}
				}
			}
			send("results", map[string]any{
				"ts":   time.Now().UTC().Format(time.RFC3339),
				"rows": snapshot,
			})
		}

		ticker := time.NewTicker(time.Duration(pollMs) * time.Millisecond)
		defer ticker.Stop()
		keepalive := time.NewTicker(15 * time.Second)
		defer keepalive.Stop()

		for {
			select {
			case <-ctx.Done():
				logLine("INFO", "results_sse_disconnect", "path=%s request_id=%s", r.URL.Path, rid)
				return
			case <-keepalive.C:
				fmt.Fprint(w, ": keepalive\n\n")
				flusher.Flush()
			case <-ticker.C:
				rows, err := fetchAggregatorResults(ctx, aggregatorURL, profileID, limit)
				if err != nil {
					send("results", map[string]any{
						"ts":    time.Now().UTC().Format(time.RFC3339),
						"error": "upstream_error",
						"rows":  []aggResult{},
					})
					continue
				}
				newRows, newest, updatedSeen := selectNewResults(rows, lastSeen, seenIDs)
				seenIDs = updatedSeen
				if newest.After(lastSeen) {
					lastSeen = newest
				}
				if len(newRows) == 0 {
					continue
				}
				send("results", map[string]any{
					"ts":   time.Now().UTC().Format(time.RFC3339),
					"rows": newRows,
				})
			}
		}
	}
	mux.HandleFunc("/api/results/stream", resultsStreamHandler)
	mux.HandleFunc("/api/live/stream", resultsStreamHandler)

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

	mux.HandleFunc("/api/summary", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
			return
		}
		if cached, ok := summary.get(); ok {
			writeJSON(w, http.StatusOK, cached)
			return
		}
		ctx := r.Context()
		data, err := buildSummary(ctx, registryURL, aggregatorURL)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "upstream_error"})
			return
		}
		summary.set(data, 10*time.Minute)
		writeJSON(w, http.StatusOK, data)
	})

	mux.HandleFunc("/api/audit/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "gateway_stub"})
	})

	mux.HandleFunc("/api/audit/v0/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
			return
		}
		limit := 200
		if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
			if n, err := strconvAtoiSafe(v); err == nil && n > 0 {
				limit = n
			}
		}
		var since time.Time
		if v := strings.TrimSpace(r.URL.Query().Get("since")); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				since = t
			}
		}
		items := audit.list(limit, since)
		writeJSON(w, http.StatusOK, map[string]any{
			"count":  len(items),
			"items":  items,
			"events": items,
		})
	})

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
		// Prefer Binance public API to auto-populate symbols even if crypto-stream is absent.
		if symbols, err := fetchBinanceSymbols(r.Context()); err == nil && len(symbols) > 0 {
			w.Header().Set("X-Source", "binance")
			writeJSON(w, http.StatusOK, symbols)
			return
		}
		// Fallback to crypto-stream if available.
		symbols, source, err := fetchCryptoSymbols(r.Context(), cryptoStreamURL)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "upstream_error", "upstream": source, "status": 0})
			return
		}
		w.Header().Set("X-Source", source)
		if source == "unavailable" {
			w.Header().Set("X-Warning", "upstream_unavailable")
		}
		writeJSON(w, http.StatusOK, symbols)
	})

	mux.HandleFunc("/api/crypto/top", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
			return
		}
		limit := clampInt(queryInt(r, "limit", 25), 1, 500)
		direction := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("direction")))
		if direction == "" {
			direction = "gainers"
		}
		suffix := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("suffix")))
		if suffix == "" {
			suffix = "USDT"
		}
		minQuote := queryFloat(r, "min_quote_vol", 0)
		rows, err := fetchBinanceTop(r.Context(), limit, direction, suffix, minQuote)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "upstream_error", "upstream": "binance", "status": 0})
			return
		}
		writeJSON(w, http.StatusOK, rows)
	})

	mux.HandleFunc("/api/crypto/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
			return
		}
		status, code, err := checkCryptoHealth(r.Context(), cryptoStreamURL)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"status": "down", "error": err.Error(), "http_status": code})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": status, "http_status": code})
	})

	mux.HandleFunc("/api/crypto/stream", func(w http.ResponseWriter, r *http.Request) {
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
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "streaming_not_supported"})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		limit := clampInt(queryInt(r, "limit", 25), 1, 500)
		direction := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("direction")))
		if direction == "" {
			direction = "gainers"
		}
		suffix := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("suffix")))
		if suffix == "" {
			suffix = "USDT"
		}
		minQuote := queryFloat(r, "min_quote_vol", 0)

		send := func(rows []cryptoTopRow, updated time.Time, errMsg string) {
			payload := map[string]any{
				"ts":      time.Now().UTC().Format(time.RFC3339),
				"updated": updated.Format(time.RFC3339),
				"rows":    rows,
			}
			if errMsg != "" {
				payload["error"] = errMsg
			}
			b, _ := json.Marshal(payload)
			fmt.Fprintf(w, "event: tickers\n")
			fmt.Fprintf(w, "data: %s\n\n", string(b))
			flusher.Flush()
		}

		ticks, updated, errMsg := crypto.snapshot()
		rows := computeTopFromTickers(ticks, limit, direction, suffix, minQuote)
		send(rows, updated, errMsg)

		ctx := r.Context()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ticks, updated, errMsg = crypto.snapshot()
				rows = computeTopFromTickers(ticks, limit, direction, suffix, minQuote)
				send(rows, updated, errMsg)
			}
		}
	})

	mux.HandleFunc("/api/gateway/connectors/catalog", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"version":    connCatalog.Version,
			"count":      len(connList),
			"connectors": connList,
		})
	})
	// Compatibility alias for older UI builds.
	mux.HandleFunc("/api/catalog", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"version":    connCatalog.Version,
			"count":      len(connList),
			"connectors": connList,
		})
	})

	mux.HandleFunc("/api/gateway/connectors/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":     "ok",
			"source":     "gateway",
			"updated_at": time.Now().UTC().Format(time.RFC3339),
			"count":      len(connList),
		})
	})
	// Compatibility alias for older UI builds.
	mux.HandleFunc("/api/connectors/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":     "ok",
			"source":     "gateway",
			"updated_at": time.Now().UTC().Format(time.RFC3339),
			"count":      len(connList),
		})
	})

	mux.HandleFunc("/api/gateway/connectors/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/gateway/connectors/")
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
			return
		}
		id := parts[0]
		if len(parts) == 2 && parts[1] == "health" {
			if !connectorExists(connCatalog, id) {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"id":         id,
				"status":     "ok",
				"updated_at": time.Now().UTC().Format(time.RFC3339),
			})
			return
		}
		if len(parts) == 2 && parts[1] == "schema" {
			if r.Method != http.MethodGet {
				writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
				return
			}
			writeJSON(w, http.StatusOK, defaultConnectorSchema(id))
			return
		}
		if len(parts) == 2 && parts[1] == "config" {
			switch r.Method {
			case http.MethodGet:
				cfg, ok := connectors.get(id)
				if !ok {
					cfg = map[string]any{"enabled": false}
				}
				writeJSON(w, http.StatusOK, map[string]any{
					"connector_id": id,
					"config":       cfg,
				})
				return
			case http.MethodPost, http.MethodPut:
				var payload map[string]any
				_ = json.NewDecoder(r.Body).Decode(&payload)
				cfg := payload["config"]
				if cfg == nil {
					cfg = payload
				}
				connectors.set(id, cfg)
				writeJSON(w, http.StatusOK, map[string]any{
					"connector_id": id,
					"config":       cfg,
					"saved_at":     time.Now().UTC().Format(time.RFC3339),
				})
				return
			default:
				writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
	})
	// Compatibility alias for older UI builds.
	mux.HandleFunc("/api/connectors/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/connectors/")
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
			return
		}
		id := parts[0]
		if len(parts) == 2 && parts[1] == "health" {
			if !connectorExists(connCatalog, id) {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"id":         id,
				"status":     "ok",
				"updated_at": time.Now().UTC().Format(time.RFC3339),
			})
			return
		}
		if len(parts) == 2 && parts[1] == "schema" {
			if r.Method != http.MethodGet {
				writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
				return
			}
			writeJSON(w, http.StatusOK, defaultConnectorSchema(id))
			return
		}
		if len(parts) == 2 && parts[1] == "config" {
			switch r.Method {
			case http.MethodGet:
				cfg, ok := connectors.get(id)
				if !ok {
					cfg = map[string]any{"enabled": false}
				}
				writeJSON(w, http.StatusOK, map[string]any{
					"connector_id": id,
					"config":       cfg,
				})
				return
			case http.MethodPost, http.MethodPut:
				var payload map[string]any
				_ = json.NewDecoder(r.Body).Decode(&payload)
				cfg := payload["config"]
				if cfg == nil {
					cfg = payload
				}
				connectors.set(id, cfg)
				writeJSON(w, http.StatusOK, map[string]any{
					"connector_id": id,
					"config":       cfg,
					"saved_at":     time.Now().UTC().Format(time.RFC3339),
				})
				return
			default:
				writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
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
	handler = withLogging(handler, audit)
	handler = withRequestID(handler)

	startEventLoops(sse, health, registryURL, aggregatorURL, coordinatorURL, reporterURL, analyticsURL)
	startCryptoCacheLoop(crypto)

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
	ID        string    `json:"id"`
	DroneID   string    `json:"drone_id"`
	ProfileID string    `json:"profile_id"`
	RunID     string    `json:"run_id"`
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
		if v, ok := row["id"].(string); ok {
			ar.ID = v
		}
		if v, ok := row["drone_id"].(string); ok {
			ar.DroneID = v
		}
		if v, ok := row["profile_id"].(string); ok {
			ar.ProfileID = v
		}
		if v, ok := row["run_id"].(string); ok {
			ar.RunID = v
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

func selectNewResults(rows []aggResult, last time.Time, seen map[string]struct{}) ([]aggResult, time.Time, map[string]struct{}) {
	if seen == nil {
		seen = make(map[string]struct{})
	}
	newest := last
	out := make([]aggResult, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		ts := getTimestamp(row, resultData(row))
		if ts.Before(last) {
			continue
		}
		if ts.Equal(last) {
			if row.ID != "" {
				if _, ok := seen[row.ID]; ok {
					continue
				}
			}
		}
		out = append(out, row)
		if ts.After(newest) {
			newest = ts
		}
	}
	if newest.After(last) {
		seen = make(map[string]struct{})
		for _, row := range out {
			ts := getTimestamp(row, resultData(row))
			if ts.Equal(newest) && row.ID != "" {
				seen[row.ID] = struct{}{}
			}
		}
	} else {
		for _, row := range out {
			if row.ID != "" {
				seen[row.ID] = struct{}{}
			}
		}
	}
	return out, newest, seen
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

func asInt(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int64:
		return int(t), true
	case float64:
		return int(t), true
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return int(n), true
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
			return n, true
		}
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
	source := "aggregator"
	if len(rowsOut) == 0 {
		fallback, ferr := fetchBinanceTop(ctx, 100, "gainers", "USDT", 0)
		if ferr == nil {
			source = "binance"
			for _, r := range fallback {
				rowsOut = append(rowsOut, rowOut{
					Symbol:    r.Symbol,
					Price:     r.Price,
					PctChange: r.PctChange,
					Volume:    r.Volume,
					QuoteVol:  r.QuoteVol,
					High:      r.High,
					Low:       r.Low,
					Open:      r.Open,
					Updated:   r.Updated,
				})
			}
		}
	}
	return map[string]any{
		"id":         "live-crypto-wall",
		"title":      "Live Crypto Wall",
		"updated_at": time.Now().UTC().Format(time.RFC3339),
		"rows":       rowsOut,
		"series":     []any{},
		"meta": map[string]any{
			"source_profiles": []string{"crypto-watchlist"},
			"window":          "last_30m",
			"source":          source,
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
	if len(points) == 0 {
		if idx, ok := buildIndexFromBinance(ctx); ok {
			points = append(points, idx)
		}
	}
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
			"/health":                         {},
			"/api/health":                     {},
			"/api/gateway/health":             {},
			"/api/status":                     {},
			"/api/events":                     {},
			"/api/live/stream":                {},
			"/api/results":                    {},
			"/api/results/summary":            {},
			"/api/results/stream":             {},
			"/api/summary":                    {},
			"/api/reports":                    {},
			"/api/audit/health":               {},
			"/api/audit/v0/events":            {},
			"/api/catalog":                    {},
			"/api/gateway/connectors/catalog": {},
			"/api/gateway/connectors/health":  {},
			"/api/connectors/health":          {},
			"/api/crypto/symbols":             {},
			"/api/crypto/top":                 {},
			"/api/crypto/stream":              {},
			"/api/crypto/health":              {},
			"/metrics":                        {},
			"/favicon.ico":                    {},
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
			if _, ok := cfg.AllowAnonymous[r.URL.Path]; ok ||
				strings.HasPrefix(r.URL.Path, "/api/reports/") ||
				strings.HasPrefix(r.URL.Path, "/api/profiles/") ||
				strings.HasPrefix(r.URL.Path, "/api/gateway/connectors/") ||
				strings.HasPrefix(r.URL.Path, "/api/connectors/") ||
				strings.HasPrefix(r.URL.Path, "/api/audit/") {
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

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
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

func withLogging(next http.Handler, audit *auditStore) http.Handler {
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
		if audit != nil {
			outcome := "success"
			if rec.status >= 400 {
				outcome = "error"
			}
			audit.add(auditEvent{
				EventID:   fmt.Sprintf("%d", time.Now().UnixNano()),
				EventTS:   ts,
				Action:    r.Method,
				Outcome:   outcome,
				ObjectKey: r.URL.Path,
				RequestID: rid,
				ActorID:   principalFromContext(r.Context()),
				Source:    "gateway",
				Detail: map[string]any{
					"status":      rec.status,
					"duration_ms": dur,
				},
			})
		}
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

func loadConnectorCatalog() connectorCatalog {
	var cat connectorCatalog
	if len(connectorCatalogYAML) == 0 {
		return cat
	}
	if err := yaml.Unmarshal(connectorCatalogYAML, &cat); err != nil {
		logLine("WARN", "connector_catalog_parse_failed", "err=%s", err.Error())
		return connectorCatalog{}
	}
	return cat
}

func buildConnectorList(cat connectorCatalog) []connectorPublic {
	out := make([]connectorPublic, 0, len(cat.Connectors))
	for _, c := range cat.Connectors {
		desc := ""
		if len(c.Endpoints) > 0 {
			desc = strings.TrimSpace(c.Endpoints[0].Notes)
		}
		pub := connectorPublic{
			ID:           strings.TrimSpace(c.ID),
			Kind:         strings.TrimSpace(c.Kind),
			DisplayName:  strings.TrimSpace(c.Name),
			Description:  desc,
			Capabilities: append([]string(nil), c.Capabilities...),
		}
		if pub.ID == "" || pub.Kind == "" || pub.DisplayName == "" {
			continue
		}
		sort.Strings(pub.Capabilities)
		out = append(out, pub)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func connectorExists(cat connectorCatalog, id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for _, c := range cat.Connectors {
		if strings.EqualFold(strings.TrimSpace(c.ID), id) {
			return true
		}
	}
	return false
}

func defaultConnectorSchema(id string) map[string]any {
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"title":   fmt.Sprintf("Connector %s", id),
		"type":    "object",
		"properties": map[string]any{
			"enabled": map[string]any{
				"type":        "boolean",
				"description": "Enable connector",
			},
			"notes": map[string]any{
				"type":        "string",
				"description": "Operator notes",
			},
		},
	}
}

// --- helpers ---

func startEventLoops(hub *sseHub, health *healthCache, reg, agg, coo, rep, ana string) {
	go func() {
		heartbeat := time.NewTicker(2 * time.Second)
		defer heartbeat.Stop()
		for range heartbeat.C {
			services := checkAllDetailed(reg, agg, coo, rep, ana).Services
			snap := health.update(services)
			hub.publish("heartbeat", map[string]any{
				"status":   snap.Status,
				"ts":       time.Now().UTC().Format(time.RFC3339),
				"services": snapshotStatusMap(snap.Services),
			})
		}
	}()

	go func() {
		tick := time.NewTicker(5 * time.Second)
		defer tick.Stop()
		for range tick.C {
			hub.publish("tick", map[string]any{"ts": time.Now().UTC().Format(time.RFC3339)})
		}
	}()

	go func() {
		var lastTotal int
		var lastIndex string
		tick := time.NewTicker(10 * time.Second)
		defer tick.Stop()
		for range tick.C {
			total, ts, ok := fetchResultSummary(context.Background(), agg)
			if ok && total != lastTotal {
				lastTotal = total
				hub.publish("results", map[string]any{
					"ts":            time.Now().UTC().Format(time.RFC3339),
					"total_results": total,
					"last_updated":  ts,
				})
			}
			if idx := fetchReportUpdated(context.Background(), agg); idx != "" && idx != lastIndex {
				lastIndex = idx
				hub.publish("insights", map[string]any{
					"ts":         time.Now().UTC().Format(time.RFC3339),
					"updated_at": idx,
				})
			}
		}
	}()
}

func startCryptoCacheLoop(cache *cryptoCache) {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ticks, err := fetchBinanceTickers(context.Background())
			if err != nil {
				cache.set(nil, err.Error())
				continue
			}
			cache.set(ticks, "")
		}
	}()
}

func parseLastEventID(v string) int64 {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, ev sseEvent) {
	if ev.Event != "" {
		if ev.ID > 0 {
			fmt.Fprintf(w, "id: %d\n", ev.ID)
		}
		fmt.Fprintf(w, "event: %s\n", ev.Event)
		fmt.Fprintf(w, "data: %s\n\n", ev.Data)
		flusher.Flush()
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func snapshotStatusMap(services map[string]serviceDetail) map[string]string {
	out := make(map[string]string, len(services))
	for k, v := range services {
		out[k] = v.Status
	}
	return out
}

func queryInt(r *http.Request, key string, def int) int {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func queryFloat(r *http.Request, key string, def float64) float64 {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return def
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return n
}

func clampInt(v, minV, maxV int) int {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func buildSummary(ctx context.Context, regURL, aggURL string) (map[string]any, error) {
	total, lastUpdated := fetchSummaryTotals(ctx, aggURL)
	profiles := fetchProfilesCount(ctx, regURL)
	return map[string]any{
		"total_results":   total,
		"active_profiles": profiles,
		"last_updated":    lastUpdated,
		"generated_at":    time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func fetchProfilesCount(ctx context.Context, regURL string) int {
	u := strings.TrimSuffix(regURL, "/") + "/profiles"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	c := &http.Client{Timeout: 4 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return 0
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0
	}
	var list []map[string]any
	if err := json.Unmarshal(body, &list); err == nil {
		return len(list)
	}
	var wrapped map[string]any
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return 0
	}
	if arr, ok := wrapped["profiles"].([]any); ok {
		return len(arr)
	}
	return 0
}

func fetchSummaryTotals(ctx context.Context, aggURL string) (int, string) {
	u := strings.TrimSuffix(aggURL, "/") + "/results/summary"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	c := &http.Client{Timeout: 4 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return 0, time.Now().UTC().Format(time.RFC3339)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return 0, time.Now().UTC().Format(time.RFC3339)
	}
	var sum map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&sum); err != nil {
		return 0, time.Now().UTC().Format(time.RFC3339)
	}
	total, _ := asInt(sum["total_results"])
	last := fetchLatestResultTS(ctx, aggURL)
	if last == "" {
		last = time.Now().UTC().Format(time.RFC3339)
	}
	return total, last
}

func fetchLatestResultTS(ctx context.Context, aggURL string) string {
	u := strings.TrimSuffix(aggURL, "/") + "/results?limit=1"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	c := &http.Client{Timeout: 4 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return ""
	}
	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil || len(rows) == 0 {
		return ""
	}
	if ts, ok := rows[0]["timestamp"].(string); ok && ts != "" {
		return ts
	}
	if ts, ok := rows[0]["created_at"].(string); ok && ts != "" {
		return ts
	}
	return ""
}

func fetchResultSummary(ctx context.Context, aggURL string) (int, string, bool) {
	total, last := fetchSummaryTotals(ctx, aggURL)
	return total, last, true
}

func fetchReportUpdated(ctx context.Context, aggURL string) string {
	rows, err := fetchAggregatorResults(ctx, aggURL, "crypto-watchlist", 1)
	if err != nil || len(rows) == 0 {
		return ""
	}
	data := resultData(rows[0])
	if data == nil {
		return ""
	}
	return getTimestamp(rows[0], data).Format(time.RFC3339)
}

func fetchCryptoSymbols(ctx context.Context, cryptoURL string) (any, string, error) {
	target := strings.TrimSuffix(cryptoURL, "/") + "/symbols"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Do(req)
	if err == nil && resp != nil {
		defer resp.Body.Close()
		if resp.StatusCode/100 == 2 {
			var payload any
			if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil {
				return payload, "crypto-stream", nil
			}
		}
	}

	return []string{}, "unavailable", nil
}

func fetchBinanceSymbols(ctx context.Context) ([]string, error) {
	// Use binance.vision to avoid geo-blocks on api.binance.com.
	u := "https://data-api.binance.vision/api/v3/exchangeInfo"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	c := &http.Client{Timeout: 6 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("non_2xx")
	}
	var info struct {
		Symbols []struct {
			Symbol string `json:"symbol"`
			Status string `json:"status"`
		} `json:"symbols"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(info.Symbols))
	for _, s := range info.Symbols {
		if s.Symbol == "" || strings.ToUpper(s.Status) != "TRADING" {
			continue
		}
		out = append(out, s.Symbol)
	}
	sort.Strings(out)
	return out, nil
}

type binanceTicker struct {
	Symbol             string `json:"symbol"`
	LastPrice          string `json:"lastPrice"`
	PriceChangePercent string `json:"priceChangePercent"`
	Volume             string `json:"volume"`
	QuoteVolume        string `json:"quoteVolume"`
	HighPrice          string `json:"highPrice"`
	LowPrice           string `json:"lowPrice"`
	OpenPrice          string `json:"openPrice"`
	CloseTime          int64  `json:"closeTime"`
}

type cryptoTopRow struct {
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

func fetchBinanceTickers(ctx context.Context) ([]binanceTicker, error) {
	// Use binance.vision to avoid geo-blocks on api.binance.com.
	u := "https://data-api.binance.vision/api/v3/ticker/24hr"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	c := &http.Client{Timeout: 6 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("non_2xx")
	}
	var ticks []binanceTicker
	if err := json.NewDecoder(resp.Body).Decode(&ticks); err != nil {
		return nil, err
	}
	return ticks, nil
}

func fetchBinanceTop(ctx context.Context, limit int, direction, suffix string, minQuote float64) ([]cryptoTopRow, error) {
	ticks, err := fetchBinanceTickers(ctx)
	if err != nil {
		return nil, err
	}
	return computeTopFromTickers(ticks, limit, direction, suffix, minQuote), nil
}

func computeTopFromTickers(ticks []binanceTicker, limit int, direction, suffix string, minQuote float64) []cryptoTopRow {
	if len(ticks) == 0 {
		return []cryptoTopRow{}
	}
	out := make([]cryptoTopRow, 0, len(ticks))
	for _, t := range ticks {
		if suffix != "" && !strings.HasSuffix(t.Symbol, suffix) {
			continue
		}
		qv, _ := asFloat(t.QuoteVolume)
		if qv < minQuote {
			continue
		}
		price, _ := asFloat(t.LastPrice)
		pct, _ := asFloat(t.PriceChangePercent)
		vol, _ := asFloat(t.Volume)
		high, _ := asFloat(t.HighPrice)
		low, _ := asFloat(t.LowPrice)
		open, _ := asFloat(t.OpenPrice)
		updated := ""
		if t.CloseTime > 0 {
			updated = time.UnixMilli(t.CloseTime).UTC().Format(time.RFC3339)
		}
		out = append(out, cryptoTopRow{
			Symbol:    t.Symbol,
			Price:     price,
			PctChange: pct,
			Volume:    vol,
			QuoteVol:  qv,
			High:      high,
			Low:       low,
			Open:      open,
			Updated:   updated,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if direction == "losers" {
			return out[i].PctChange < out[j].PctChange
		}
		return out[i].PctChange > out[j].PctChange
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func buildIndexFromBinance(ctx context.Context) (struct {
	T string  `json:"t"`
	Y float64 `json:"y"`
}, bool) {
	ticks, err := fetchBinanceTickers(ctx)
	if err != nil || len(ticks) == 0 {
		return struct {
			T string  `json:"t"`
			Y float64 `json:"y"`
		}{}, false
	}
	type ranked struct {
		price float64
		qv    float64
	}
	top := make([]ranked, 0, 50)
	for _, t := range ticks {
		if !strings.HasSuffix(t.Symbol, "USDT") {
			continue
		}
		qv, _ := asFloat(t.QuoteVolume)
		price, _ := asFloat(t.LastPrice)
		if qv <= 0 || price <= 0 {
			continue
		}
		top = append(top, ranked{price: price, qv: qv})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].qv > top[j].qv })
	if len(top) > 10 {
		top = top[:10]
	}
	if len(top) == 0 {
		return struct {
			T string  `json:"t"`
			Y float64 `json:"y"`
		}{}, false
	}
	var sum float64
	for _, r := range top {
		sum += r.price
	}
	return struct {
		T string  `json:"t"`
		Y float64 `json:"y"`
	}{T: time.Now().UTC().Format(time.RFC3339), Y: sum / float64(len(top))}, true
}

func checkCryptoHealth(ctx context.Context, cryptoURL string) (string, int, error) {
	target := strings.TrimSuffix(cryptoURL, "/") + "/health"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return "down", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "down", resp.StatusCode, fmt.Errorf("non_2xx")
	}
	return "up", resp.StatusCode, nil
}

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
