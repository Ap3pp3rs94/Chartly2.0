package api

import (
	"encoding/json"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/Ap3pp3rs94/Chartly2.0/services/gateway/api/handlers"
)

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	var eb errorBody
	eb.Error.Code = code
	eb.Error.Message = message
	_ = json.NewEncoder(w).Encode(eb)
}

func requireJSON(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("content-type")
		if ct == "" {
			writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "content-type is required for this endpoint")
			return
		}

		// Allow parameters e.g. application/json; charset=utf-8
		if !strings.HasPrefix(strings.ToLower(ct), "application/json") {
			writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "content-type must be application/json")
			return
		}
		next(w, r)
	}
}

func methodOnly(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("allow", method)
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		next(w, r)
	}
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				_ = debug.Stack()
				writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// NewRouter returns the gateway API router. All handlers should return JSON.
// Auth, rate limiting, CORS, and request_id are handled by outer middleware layers.
func NewRouter() http.Handler {
	mux := http.NewServeMux()

	// Health / readiness
	mux.HandleFunc("/health", methodOnly(http.MethodGet, handlers.Health))
	mux.HandleFunc("/ready", methodOnly(http.MethodGet, handlers.Health))

	// Sources
	mux.HandleFunc("/sources", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			requireJSON(handlers.SourcesCreate)(w, r)
		case http.MethodGet:
			handlers.SourcesList(w, r)
		default:
			w.Header().Set("allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	})

	// Ingestion enqueue (simple endpoint for scheduling)
	mux.HandleFunc("/sources/enqueue", methodOnly(http.MethodPost, requireJSON(handlers.IngestionEnqueue)))

	// Query (placeholder)
	mux.HandleFunc("/query", methodOnly(http.MethodGet, handlers.Query))

	// Reports (placeholder)
	mux.HandleFunc("/reports", methodOnly(http.MethodPost, requireJSON(handlers.Reports)))

	return recoverer(mux)
}
