package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ServeEvents handles GET /api/events (SSE)
func ServeEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "no_flusher", "streaming unsupported")
		return
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	lastCatalog := ""
	lastResults := ""

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			data, _ := buildHeartbeat(r)
			payload, _ := json.Marshal(data)
			fmt.Fprintf(w, "event: heartbeat\n")
			fmt.Fprintf(w, "data: %s\n\n", payload)

			if data.CatalogHash != "" && data.CatalogHash != lastCatalog {
				lastCatalog = data.CatalogHash
				fmt.Fprintf(w, "event: catalog_changed\n")
				fmt.Fprintf(w, "data: {\"catalog_hash\":\"%s\"}\n\n", data.CatalogHash)
			}
			if data.LatestResultTS != "" && data.LatestResultTS != lastResults {
				lastResults = data.LatestResultTS
				fmt.Fprintf(w, "event: results_changed\n")
				fmt.Fprintf(w, "data: {\"latest_result_ts\":\"%s\"}\n\n", data.LatestResultTS)
			}
			flusher.Flush()
		}
	}
}
