package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultPort = "8080"

	defaultRegistryURL    = "http://registry:8081"
	defaultAggregatorURL  = "http://aggregator:8082"
	defaultCoordinatorURL = "http://coordinator:8083"
	defaultReporterURL    = "http://reporter:8084"

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

func main() {
	registryURL := envOr("REGISTRY_URL", defaultRegistryURL)
	aggregatorURL := envOr("AGGREGATOR_URL", defaultAggregatorURL)
	coordinatorURL := envOr("COORDINATOR_URL", defaultCoordinatorURL)
	reporterURL := envOr("REPORTER_URL", defaultReporterURL)

	regProxy := mustProxy(registryURL)
	aggProxy := mustProxy(aggregatorURL)
	cooProxy := mustProxy(coordinatorURL)
	repProxy := mustProxy(reporterURL)

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

		sum := checkAll(registryURL, aggregatorURL, coordinatorURL, reporterURL)
		status := "healthy"
		if sum["services"].(map[string]string)["registry"] != "up" ||
			sum["services"].(map[string]string)["aggregator"] != "up" ||
			sum["services"].(map[string]string)["coordinator"] != "up" ||
			sum["services"].(map[string]string)["reporter"] != "up" {
			status = "degraded"
		}
		sum["status"] = status
		writeJSON(w, http.StatusOK, sum)
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

		out := checkAllDetailed(registryURL, aggregatorURL, coordinatorURL, reporterURL)
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

	mux.Handle("/api/reports/", stripPrefixProxy("/api", repProxy))
	mux.Handle("/api/reports", stripPrefixProxy("/api", repProxy))

	// Static + SPA fallback (everything else)
	mux.HandleFunc("/", serveSPA(distDir))

	// Middleware order: X-Request-ID -> Logging -> CORS
	var handler http.Handler = mux
	handler = withCORS(handler)
	handler = withLogging(handler)
	handler = withRequestID(handler)

	addr := ":" + defaultPort
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	logLine("INFO", "starting", "addr=%s registry=%s aggregator=%s coordinator=%s reporter=%s", addr, registryURL, aggregatorURL, coordinatorURL, reporterURL)
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

func checkAll(reg, agg, coo, rep string) map[string]any {
	svcs := map[string]string{
		"registry":    upOrDown(reg + "/health"),
		"aggregator":  upOrDown(agg + "/health"),
		"coordinator": upOrDown(coo + "/health"),
		"reporter":    upOrDown(rep + "/health"),
	}
	return map[string]any{
		"status":   "healthy",
		"services": svcs,
	}
}

func checkAllDetailed(reg, agg, coo, rep string) statusDetailed {
	return statusDetailed{
		Status: "healthy",
		Services: map[string]serviceDetail{
			"registry":    upOrDownDetailed(reg + "/health"),
			"aggregator":  upOrDownDetailed(agg + "/health"),
			"coordinator": upOrDownDetailed(coo + "/health"),
			"reporter":    upOrDownDetailed(rep + "/health"),
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
		fmt.Fprintf(os.Stdout, "%s method=%s path=%s status=%d duration_ms=%d\n",
			ts, r.Method, r.URL.Path, rec.status, dur)
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID, X-API-Key")
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
