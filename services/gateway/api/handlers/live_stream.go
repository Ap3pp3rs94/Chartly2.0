package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type liveHello struct {
	OK              bool   `json:"ok"`
	ServerVersion   string `json:"server_version"`
	IntervalMS      int    `json:"interval_ms"`
	SummaryInterval int    `json:"summary_interval_ms"`
}

type liveHeartbeat struct {
	TS    string `json:"ts"`
	Alive bool   `json:"alive"`
}

type liveError struct {
	TS      string `json:"ts"`
	Message string `json:"message"`
}

// LiveStream handles GET /api/live/stream (SSE)
func LiveStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "no_flusher", "streaming unsupported")
		return
	}

	q := r.URL.Query()
	intervalMS := clampInt(parseInt(q.Get("interval_ms"), 1000), 250, 5000)
	limit := clampInt(parseInt(q.Get("limit"), 200), 50, 1000)
	profiles := parseCSV(q.Get("profiles"))

	baseURL := "http://" + r.Host
	if r.TLS != nil {
		baseURL = "https://" + r.Host
	}

	if len(profiles) == 0 {
		ids, err := listProfileIDs(baseURL)
		if err == nil {
			profiles = ids
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	sendEvent(w, flusher, "hello", liveHello{OK: true, ServerVersion: "v1", IntervalMS: intervalMS, SummaryInterval: 600000})

	lastSent := time.Now()
	lastHB := time.Now()

	seen := make(map[string]struct{})
	order := make([]string, 0, 2000)

	ticker := time.NewTicker(time.Duration(intervalMS) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if time.Since(lastHB) >= time.Duration(intervalMS)*time.Millisecond {
				sendEvent(w, flusher, "heartbeat", liveHeartbeat{TS: time.Now().UTC().Format(time.RFC3339), Alive: true})
				lastHB = time.Now()
				lastSent = time.Now()
			}

			items := fetchResultsBatch(baseURL, profiles, limit)
			if len(items) > 0 {
				fresh := make([]map[string]any, 0, len(items))
				for _, it := range items {
					id := resultID(it)
					if id == "" {
						continue
					}
					if _, ok := seen[id]; ok {
						continue
					}
					seen[id] = struct{}{}
					order = append(order, id)
					if len(order) > 2000 {
						drop := order[0]
						order = order[1:]
						delete(seen, drop)
					}
					fresh = append(fresh, it)
				}
				if len(fresh) > 0 {
					payload := map[string]any{
						"ts":    time.Now().UTC().Format(time.RFC3339),
						"items": fresh,
					}
					sendEvent(w, flusher, "results", payload)
					lastSent = time.Now()
				}
			}

			if time.Since(lastSent) >= 15*time.Second {
				fmt.Fprintf(w, ": keepalive\n\n")
				flusher.Flush()
				lastSent = time.Now()
			}
		}
	}
}

func sendEvent(w http.ResponseWriter, flusher http.Flusher, name string, v any) {
	b, _ := json.Marshal(v)
	fmt.Fprintf(w, "event: %s\n", name)
	fmt.Fprintf(w, "data: %s\n\n", strings.TrimSpace(string(b)))
	flusher.Flush()
}

func listProfileIDs(baseURL string) ([]string, error) {
	body, err := fetchJSON(baseURL+"/api/profiles", 2*time.Second)
	if err != nil {
		return nil, err
	}
	var v struct {
		Profiles []struct {
			ID string `json:"id"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(v.Profiles))
	for _, p := range v.Profiles {
		id := strings.TrimSpace(p.ID)
		if id != "" {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}

func fetchResultsBatch(baseURL string, profiles []string, limit int) []map[string]any {
	if len(profiles) == 0 {
		arr, _ := fetchResults(baseURL+"/api/results?limit="+strconv.Itoa(limit), limit)
		return arr
	}
	out := make([]map[string]any, 0, limit)
	for _, id := range profiles {
		url := baseURL + "/api/results?profile_id=" + id + "&limit=" + strconv.Itoa(limit)
		arr, err := fetchResults(url, limit)
		if err == nil && len(arr) > 0 {
			out = append(out, arr...)
		}
	}
	return out
}

func fetchResults(url string, limit int) ([]map[string]any, error) {
	body, err := fetchJSON(url, 2*time.Second)
	if err != nil {
		return nil, err
	}
	var arr []map[string]any
	if err := json.Unmarshal(body, &arr); err != nil {
		return nil, err
	}
	if limit > 0 && len(arr) > limit {
		return arr[:limit], nil
	}
	return arr, nil
}

func resultID(m map[string]any) string {
	if v, ok := m["id"].(string); ok && v != "" {
		return v
	}
	if v, ok := m["record_id"].(string); ok && v != "" {
		return v
	}
	if v, ok := m["timestamp"].(string); ok && v != "" {
		return v
	}
	b, _ := json.Marshal(m)
	if len(b) == 0 {
		return ""
	}
	return string(b)
}

func parseInt(v string, def int) int {
	if strings.TrimSpace(v) == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func parseCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

