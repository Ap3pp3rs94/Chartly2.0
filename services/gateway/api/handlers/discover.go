package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	discoverTTL      = 10 * time.Minute
	discoverMaxRows  = 100
	discoverTimeout  = 15 * time.Second
	discoverMaxBytes = 2 << 20
)

type discoverResp struct {
	Source   string          `json:"source"`
	Query    string          `json:"query"`
	Profiles []discoverProfile `json:"profiles"`
}

type discoverProfile struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Version     string         `json:"version"`
	Description string         `json:"description"`
	Source      profileSource  `json:"source"`
	Mapping     map[string]string `json:"mapping"`
}

type profileSource struct {
	Type string `json:"type"`
	URL  string `json:"url"`
	Auth string `json:"auth"`
}

type discoverCacheEntry struct {
	resp      discoverResp
	expiresAt time.Time
}

var (
	discoverMu    sync.Mutex
	discoverCache = map[string]discoverCacheEntry{}
)

// DiscoverDataGov handles GET /api/discover/datagov
func DiscoverDataGov(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		q = "transit"
	}
	rows := 25
	if v := strings.TrimSpace(r.URL.Query().Get("rows")); v != "" {
		if n, err := parseInt(v); err == nil {
			rows = n
		}
	}
	if rows < 1 {
		rows = 1
	}
	if rows > discoverMaxRows {
		rows = discoverMaxRows
	}

	cacheKey := strings.ToLower(strings.TrimSpace(q)) + "|" + fmt.Sprintf("%d", rows)
	if resp, ok := discoverCacheGet(cacheKey); ok {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	resp, err := fetchDataGov(q, rows)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "discover_failed", "data.gov discovery failed")
		return
	}
	resp.Source = "data.gov"
	resp.Query = q

	discoverCacheSet(cacheKey, resp)
	writeJSON(w, http.StatusOK, resp)
}

func fetchDataGov(q string, rows int) (discoverResp, error) {
	u := "https://api.gsa.gov/technology/datagov/v3/action/package_search"
	params := url.Values{}
	params.Set("q", q)
	params.Set("rows", fmt.Sprintf("%d", rows))
	full := u + "?" + params.Encode()

	req, _ := http.NewRequest(http.MethodGet, full, nil)
	if key := strings.TrimSpace(os.Getenv("DATA_GOV_API_KEY")); key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	client := &http.Client{Timeout: discoverTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return discoverResp{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return discoverResp{}, fmt.Errorf("auth_required")
	}
	if resp.StatusCode/100 != 2 {
		return discoverResp{}, fmt.Errorf("http_%d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, discoverMaxBytes))
	if err != nil {
		return discoverResp{}, err
	}

	var raw datagovResponse
	if err := json.Unmarshal(b, &raw); err != nil {
		return discoverResp{}, err
	}

	profiles := make([]discoverProfile, 0, len(raw.Result.Results))
	for _, ds := range raw.Result.Results {
		best := selectResource(ds.Resources)
		if best == "" {
			continue
		}
		id := "datagov-" + slug(ds.Name)
		if id == "datagov-" {
			continue
		}
		p := discoverProfile{
			ID:          id,
			Name:        strings.TrimSpace(ds.Title),
			Version:     "1.0.0",
			Description: "Discovered from Data.gov catalog",
			Source: profileSource{Type: "http_rest", URL: best, Auth: "none"},
			Mapping: map[string]string{"raw": "raw"},
		}
		if p.Name == "" {
			p.Name = id
		}
		profiles = append(profiles, p)
	}

	sort.Slice(profiles, func(i, j int) bool { return profiles[i].ID < profiles[j].ID })
	return discoverResp{Profiles: profiles}, nil
}

type datagovResponse struct {
	Result struct {
		Results []struct {
			Name      string `json:"name"`
			Title     string `json:"title"`
			Resources []struct {
				URL     string `json:"url"`
				Format  string `json:"format"`
				Mimetype string `json:"mimetype"`
			} `json:"resources"`
		} `json:"results"`
	} `json:"result"`
}

func selectResource(resources []struct {
	URL     string `json:"url"`
	Format  string `json:"format"`
	Mimetype string `json:"mimetype"`
}) string {
	bestJSON := ""
	bestCSV := ""
	bestAny := ""
	for _, r := range resources {
		url := strings.TrimSpace(r.URL)
		if url == "" {
			continue
		}
		if bestAny == "" {
			bestAny = url
		}
		fmtl := strings.ToLower(strings.TrimSpace(r.Format))
		mt := strings.ToLower(strings.TrimSpace(r.Mimetype))
		if bestJSON == "" && (strings.Contains(fmtl, "json") || strings.Contains(mt, "json")) {
			bestJSON = url
			continue
		}
		if bestCSV == "" && (strings.Contains(fmtl, "csv") || strings.Contains(mt, "csv")) {
			bestCSV = url
		}
	}
	if bestJSON != "" {
		return bestJSON
	}
	if bestCSV != "" {
		return bestCSV
	}
	return bestAny
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	b := strings.Builder{}
	lastDash := false
	for _, r := range s {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlnum {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	return out
}

func parseInt(s string) (int, error) {
	var n int
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

func discoverCacheGet(key string) (discoverResp, bool) {
	discoverMu.Lock()
	defer discoverMu.Unlock()
	if e, ok := discoverCache[key]; ok {
		if time.Now().Before(e.expiresAt) {
			return e.resp, true
		}
		delete(discoverCache, key)
	}
	return discoverResp{}, false
}

func discoverCacheSet(key string, resp discoverResp) {
	discoverMu.Lock()
	defer discoverMu.Unlock()
	discoverCache[key] = discoverCacheEntry{
		resp:      resp,
		expiresAt: time.Now().Add(discoverTTL),
	}
}
