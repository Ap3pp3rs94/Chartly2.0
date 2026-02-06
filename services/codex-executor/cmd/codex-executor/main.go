package main

import (
	"encoding/json"
	"net/http"
	"time"
)

type execRequest struct {
	RunnerID  string `json:"runner_id"`
	ProfileID string `json:"profile_id"`
	Prompt    string `json:"prompt"`
}

type execResponse struct {
	Ok     bool   `json:"ok"`
	Output string `json:"output"`
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "healthy"})
	})
	mux.HandleFunc("/execute", handleExecute)

	srv := &http.Server{
		Addr:              ":8087",
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	_ = srv.ListenAndServe()
}

func handleExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	var req execRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	_ = r.Body.Close()

	output := map[string]any{
		"ok":      true,
		"records": []any{},
		"notes":   []any{"stub_executor"},
		"stats":   map[string]any{"records_in": 0, "records_out": 0},
	}
	b, _ := json.Marshal(output)
	writeJSON(w, http.StatusOK, execResponse{Ok: true, Output: string(b)})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
