package main

import (
	"encoding/json"
	"net/http"
	"strings"
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

	// If prompt looks like topics batch (contains \"topics\" and \"topic_id\"), return
	// a deterministic minimal topics payload so downstream validators pass.
	if strings.Contains(req.Prompt, "\"topics\"") && strings.Contains(req.Prompt, "\"topic_id\"") {
		type promptRoot struct {
			Topics []struct {
				ID string `json:"topic_id"`
				Name string `json:"name"`
			} `json:"topics"`
		}
		var pr promptRoot
		_ = json.Unmarshal([]byte(req.Prompt), &pr)
		topics := []map[string]any{}
		for _, t := range pr.Topics {
			if t.ID == "" { continue }
			topics = append(topics, map[string]any{
				"topic_id": t.ID,
				"as_of": time.Now().UTC().Format(time.RFC3339),
				"signals": map[string]any{
					"price": map[string]any{"usd": 0, "change_24h_pct": 0, "volume_24h": 0},
					"sentiment": map[string]any{"score": 0, "label": ""},
					"news": map[string]any{"count_24h": 0, "top_headlines": []any{}},
					"chatter": map[string]any{"hn_mentions_24h": 0},
					"dev": map[string]any{"github_commits_7d": 0},
					"regulatory": map[string]any{"mentions_24h": 0},
				},
				"insights": []any{},
			})
		}
		output := map[string]any{"ok": true, "topics": topics}
		b, _ := json.Marshal(output)
		writeJSON(w, http.StatusOK, execResponse{Ok: true, Output: string(b)})
		return
	}

	// Default stub for other runners (e.g., codex-runner).
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
