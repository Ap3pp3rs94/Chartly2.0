package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ap3pp3rs94/Chartly2.0/services/codex-runner/internal/runner"
)

func main() {
	cfg := runner.LoadConfig()
	r := runner.NewRunner(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", r.HandleHealth)
	mux.HandleFunc("/ready", r.HandleReady)
	mux.HandleFunc("/metrics", r.HandleMetrics)
	mux.HandleFunc("/runs/recent", r.HandleRunsRecent)
	mux.HandleFunc("/runs/trigger", r.HandleRunsTrigger)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	r.Start()
	go func() { _ = srv.ListenAndServe() }()

	<-ctx.Done()
	ctxShutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctxShutdown)
	r.Stop()
}
