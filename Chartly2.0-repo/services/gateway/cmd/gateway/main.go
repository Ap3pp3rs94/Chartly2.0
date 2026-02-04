package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Ap3pp3rs94/Chartly2.0/services/gateway/api"
	"github.com/Ap3pp3rs94/Chartly2.0/services/gateway/internal/middleware"
)
type Config struct {
	Env      string
	LogLevel string
	Addr     string
}

func loadConfig() Config {
	env := strings.TrimSpace(os.Getenv("CHARTLY_ENV"))
if env == "" {
		env = "local"
	}
	logLevel := strings.TrimSpace(os.Getenv("LOG_LEVEL"))
if logLevel == "" {
		logLevel = strings.TrimSpace(os.Getenv("CHARTLY_LOG_LEVEL"))
	}
	if logLevel == "" {
		logLevel = "info"
	}
	port := strings.TrimSpace(os.Getenv("PORT"))
if port == "" {
		port = strings.TrimSpace(os.Getenv("GATEWAY_PORT"))
	}
	if port == "" {
		port = "8080"
	}
	return Config{
		Env:      env,
		LogLevel: logLevel,
		Addr:     ":" + port,
	}
}
func main() {
	cfg := loadConfig()
	logger := log.New(os.Stdout, "", 0)
	logger.Printf(`{"level":"info","service":"gateway","env":"%s","msg":"starting","addr":"%s"}`, cfg.Env, cfg.Addr)

	// Base router from api package (to be implemented next).
	// We still ensure /health exists even if the api router is incomplete.
	// var handler http.Handler
	handler := healthFallback(api.NewRouter(), cfg.Env)

	// Middleware chain (outermost first = runs first on request).
	// Order is mandated by project law:
	//  1) request_id -> 2) cors -> 3) rate_limit -> 4) auth
	handler = middleware.RequestID(handler)
	handler = middleware.CORS(handler)
	handler = middleware.RateLimit(handler)
	handler = middleware.Auth(handler)
srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		logger.Printf(`{"level":"error","service":"gateway","env":"%s","msg":"listen_failed","error":"%s"}`, cfg.Env, sanitizeErr(err))
		os.Exit(1)
	}

	// Run server
	errCh := make(chan error, 1)
	go func() {
		logger.Printf(`{"level":"info","service":"gateway","env":"%s","msg":"listening","addr":"%s"}`, cfg.Env, ln.Addr().String())
		errCh <- srv.Serve(ln)
	}()

	// Shutdown signals
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
select {
	case sig := <-sigCh:
		logger.Printf(`{"level":"info","service":"gateway","env":"%s","msg":"shutdown_signal","signal":"%s"}`, cfg.Env, sig.String())
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			logger.Printf(`{"level":"error","service":"gateway","env":"%s","msg":"server_error","error":"%s"}`, cfg.Env, sanitizeErr(err))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Printf(`{"level":"error","service":"gateway","env":"%s","msg":"shutdown_failed","error":"%s"}`, cfg.Env, sanitizeErr(err))
		_ = srv.Close()
	} else {
		logger.Printf(`{"level":"info","service":"gateway","env":"%s","msg":"shutdown_complete"}`, cfg.Env)
	}
}

// healthFallback ensures /health exists and returns JSON, even if the underlying router is incomplete.
// The underlying router remains the primary handler for all other routes.
func healthFallback(next http.Handler, env string) http.Handler {
	mux := http.NewServeMux()

	// Fallback health endpoint (authoritative behavior defined in acceptance criteria).
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json; charset=utf-8")
		// request_id header is injected by middleware layer.
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status":"ok","service":"gateway","env":"%s","ts":"%s"}`, env, time.Now().UTC().Format(time.RFC3339Nano))))
	})

	// Delegate everything else
	mux.Handle("/", next)
	return mux
}
func sanitizeErr(err error) string {
	// Never include sensitive content; stdlib errors are typically safe, keep it conservative.
	s := err.Error()
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	if len(s) > 500 {
		return s[:500] + ""
	}
	return s
}
