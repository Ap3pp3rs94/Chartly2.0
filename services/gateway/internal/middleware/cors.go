package middleware

import (
	"net/http"
	"os"
	"strconv"
	"strings"
)

type corsConfig struct {
	allowedOrigins   []string
	allowedMethods   string
	allowedHeaders   string
	allowCredentials bool
	maxAgeSeconds    int
	allowAllOrigins  bool
}

func loadCORSConfig() corsConfig {
	origins := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if origins == "" {
		origins = "*"
	}
	allowedOrigins := splitCSV(origins)
	methods := strings.TrimSpace(os.Getenv("CORS_ALLOWED_METHODS"))
	if methods == "" {
		methods = "GET,POST,PUT,PATCH,DELETE,OPTIONS"
	}
	headers := strings.TrimSpace(os.Getenv("CORS_ALLOWED_HEADERS"))
	if headers == "" {
		headers = "*"
	}
	cred := strings.TrimSpace(os.Getenv("CORS_ALLOW_CREDENTIALS"))
	allowCred := false
	if cred != "" {
		allowCred = strings.EqualFold(cred, "true")
	}
	maxAge := 600
	if v := strings.TrimSpace(os.Getenv("CORS_MAX_AGE_SECONDS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			maxAge = n
		}
	}
	allowAll := false
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAll = true
			break
		}
	}
	return corsConfig{
		allowedOrigins:   allowedOrigins,
		allowedMethods:   methods,
		allowedHeaders:   headers,
		allowCredentials: allowCred,
		maxAgeSeconds:    maxAge,
		allowAllOrigins:  allowAll,
	}
}
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return []string{"*"}
	}
	return out
}
func originAllowed(cfg corsConfig, origin string) (string, bool) {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return "", false
	}
	if cfg.allowCredentials {
		// With credentials, we cannot use wildcard. Must explicitly allow origin.
		for _, o := range cfg.allowedOrigins {
			if o == origin {
				return origin, true
			}
		}
		return "", false
	}

	// No credentials
	if cfg.allowAllOrigins {
		return "*", true
	}
	for _, o := range cfg.allowedOrigins {
		if o == origin {
			return origin, true
		}
	}
	return "", false
}
func setCORSHeaders(w http.ResponseWriter, cfg corsConfig, allowedOrigin string) {
	if allowedOrigin != "" {
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)

		// Ensure caches differentiate by Origin when not wildcard
		if allowedOrigin != "*" {
			w.Header().Add("Vary", "Origin")
		}
	}
	w.Header().Set("Access-Control-Allow-Methods", cfg.allowedMethods)
	w.Header().Set("Access-Control-Allow-Headers", cfg.allowedHeaders)
	if cfg.allowCredentials {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
	w.Header().Set("Access-Control-Max-Age", strconv.Itoa(cfg.maxAgeSeconds))
}
func CORS(next http.Handler) http.Handler {
	cfg := loadCORSConfig()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowedOrigin, ok := originAllowed(cfg, origin)

		// If Origin isn't allowed, do not set allow headers; still serve request (common pattern).
		// Security-sensitive deployments can tighten this at gateway config layer later.
		if ok {
			setCORSHeaders(w, cfg, allowedOrigin)
		}

		// Preflight handling
		if r.Method == http.MethodOptions {
			// Always respond 204 to preflight; CORS headers are set only when origin allowed.
			w.WriteHeader(http.StatusNoContent)
			// return
		}
		next.ServeHTTP(w, r)
	})
}
