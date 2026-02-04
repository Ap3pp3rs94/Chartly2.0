package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"time"
)

type heartbeatData struct {
	Status         string            `json:"status"`
	TS             string            `json:"ts"`
	Services       map[string]string `json:"services"`
	Counts         map[string]int    `json:"counts"`
	CatalogHash    string            `json:"catalog_hash"`
	LatestResultTS string            `json:"latest_result_ts"`
}

// GetHeartbeat handles GET /api/heartbeat with ETag.
func GetHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	data, etag := buildHeartbeat(r)
	if inm := r.Header.Get("If-None-Match"); inm != "" && inm == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)
	writeJSON(w, http.StatusOK, data)
}

func buildHeartbeat(r *http.Request) (heartbeatData, string) {
	baseURL := "http://" + r.Host
	if r.TLS != nil {
		baseURL = "https://" + r.Host
	}

	services := map[string]string{
		"registry":    checkService(baseURL + "/api/profiles"),
		"aggregator":  checkService(baseURL + "/api/results/summary"),
		"coordinator": checkService(baseURL + "/api/drones"),
	}

	upCount := 0
	unknownCount := 0
	for _, v := range services {
		switch v {
		case "up":
			upCount++
		case "unknown":
			unknownCount++
		}
	}

	status := "degraded"
	if upCount >= 2 {
		status = "healthy"
	} else if upCount == 0 && unknownCount == 0 {
		status = "down"
	}

	counts := map[string]int{"profiles": 0, "drones": 0, "results": 0}
	if p, err := fetchProfilesCount(baseURL + "/api/profiles"); err == nil {
		counts["profiles"] = p
	}
	if d, err := fetchDronesCount(baseURL + "/api/drones"); err == nil {
		counts["drones"] = d
	}
	if rcount, err := fetchResultsCount(baseURL + "/api/results/summary"); err == nil {
		counts["results"] = rcount
	}

	_, catBytes := getCatalogCached()
	catalogHash := hashBytes(catBytes)

	latestTS := fetchLatestResultTS(baseURL + "/api/results?limit=1")

	resp := heartbeatData{
		Status:         status,
		TS:             time.Now().UTC().Format(time.RFC3339),
		Services:       services,
		Counts:         counts,
		CatalogHash:    catalogHash,
		LatestResultTS: latestTS,
	}

	etag := buildHeartbeatETag(resp)
	return resp, etag
}

func buildHeartbeatETag(h heartbeatData) string {
	clone := h
	clone.TS = ""
	b, _ := json.Marshal(clone)
	return hashBytes(b)
}

func checkService(url string) string {
	st, err := simpleGet(url)
	if err != nil {
		return "down"
	}
	if st == http.StatusNotFound || st == http.StatusNotImplemented {
		return "unknown"
	}
	if st/100 == 2 {
		return "up"
	}
	return "down"
}

func simpleGet(url string) (int, error) {
	client := &http.Client{Timeout: 2 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func fetchProfilesCount(url string) (int, error) {
	body, err := fetchJSON(url, 2*time.Second)
	if err != nil {
		return 0, err
	}
	var v struct {
		Profiles []struct {
			ID string `json:"id"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return 0, err
	}
	return len(v.Profiles), nil
}

func fetchDronesCount(url string) (int, error) {
	body, err := fetchJSON(url, 2*time.Second)
	if err != nil {
		return 0, err
	}
	var arr []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &arr); err != nil {
		return 0, err
	}
	return len(arr), nil
}

func fetchResultsCount(url string) (int, error) {
	body, err := fetchJSON(url, 2*time.Second)
	if err != nil {
		return 0, err
	}
	var v struct {
		Total int `json:"total_results"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return 0, err
	}
	return v.Total, nil
}

func fetchLatestResultTS(url string) string {
	body, err := fetchJSON(url, 2*time.Second)
	if err != nil {
		return ""
	}
	var arr []map[string]interface{}
	if err := json.Unmarshal(body, &arr); err != nil {
		return ""
	}
	if len(arr) == 0 {
		return ""
	}
	if ts, ok := arr[0]["timestamp"].(string); ok {
		return ts
	}
	return ""
}

func fetchJSON(url string, timeout time.Duration) ([]byte, error) {
	client := &http.Client{Timeout: timeout}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, http.ErrHandlerTimeout
	}
	return b, nil
}
