package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	catalogCacheTTL      = 5 * time.Minute
	catalogMaxProfiles   = 200
	catalogMaxFields     = 500
	catalogMaxCandidates = 50
)

type catalogResp struct {
	GeneratedAt      string           `json:"generated_at"`
	ExpiresInSeconds int64            `json:"expires_in_seconds"`
	Profiles         []catalogProfile `json:"profiles"`
}

type catalogProfile struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Fields     []profileField   `json:"fields"`
	Candidates candidateBuckets `json:"candidates"`
}

type candidateBuckets struct {
	Time   []candidateField `json:"time"`
	Join   []candidateField `json:"join"`
	Metric []candidateField `json:"metric"`
	Geo    []candidateField `json:"geo"`
}

type candidateField struct {
	Path  string `json:"path"`
	Label string `json:"label"`
	Type  string `json:"type"`
	Score int    `json:"-"`
}

type catalogCacheEntry struct {
	resp      catalogResp
	etag      string
	bytes     []byte
	expiresAt time.Time
}

var (
	catalogMu    sync.Mutex
	catalogCache *catalogCacheEntry
)

// GetCatalog handles GET /api/catalog with ETag + cache.
func GetCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	resp, etag := getCatalogCached()
	if inm := strings.TrimSpace(r.Header.Get("If-None-Match")); inm != "" && inm == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)
	writeJSON(w, http.StatusOK, resp)
}

func getCatalogCached() (catalogResp, string) {
	catalogMu.Lock()
	defer catalogMu.Unlock()

	now := time.Now()
	if catalogCache != nil && now.Before(catalogCache.expiresAt) {
		resp := catalogCache.resp
		resp.ExpiresInSeconds = int64(time.Until(catalogCache.expiresAt).Seconds())
		return resp, catalogCache.etag
	}

	resp := buildCatalog()
	resp.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	resp.ExpiresInSeconds = int64(catalogCacheTTL.Seconds())

	etag := buildCatalogETag(resp)
	b, _ := json.Marshal(resp)
	catalogCache = &catalogCacheEntry{resp: resp, etag: etag, bytes: b, expiresAt: time.Now().Add(catalogCacheTTL)}
	return resp, etag
}

func buildCatalogETag(resp catalogResp) string {
	clone := resp
	clone.GeneratedAt = ""
	clone.ExpiresInSeconds = int64(catalogCacheTTL.Seconds())
	b, _ := json.Marshal(clone)
	return hashBytes(b)
}

func buildCatalog() catalogResp {
	items, _ := listProfilesInternal()
	if len(items) > catalogMaxProfiles {
		items = items[:catalogMaxProfiles]
	}

	profiles := make([]catalogProfile, 0, len(items))
	for _, it := range items {
		fields := getCachedFieldsForProfile(it.ID)
		if len(fields) > catalogMaxFields {
			fields = fields[:catalogMaxFields]
		}
		cand := buildCandidates(fields)
		capCandidates(&cand)

		profiles = append(profiles, catalogProfile{
			ID:         it.ID,
			Name:       it.Name,
			Fields:     fields,
			Candidates: cand,
		})
	}
	return catalogResp{Profiles: profiles}
}

func capCandidates(c *candidateBuckets) {
	if len(c.Time) > catalogMaxCandidates {
		c.Time = c.Time[:catalogMaxCandidates]
	}
	if len(c.Join) > catalogMaxCandidates {
		c.Join = c.Join[:catalogMaxCandidates]
	}
	if len(c.Metric) > catalogMaxCandidates {
		c.Metric = c.Metric[:catalogMaxCandidates]
	}
	if len(c.Geo) > catalogMaxCandidates {
		c.Geo = c.Geo[:catalogMaxCandidates]
	}
}

func listProfilesInternal() ([]profileListItem, error) {
	root, err := findRepoRoot()
	if err != nil {
		return nil, err
	}
	files, err := filepath.Glob(filepath.Join(root, "profiles", "government", "*.yaml"))
	if err != nil {
		return nil, err
	}
	items := make([]profileListItem, 0, len(files))
	for _, p := range files {
		id, name := loadProfileMeta(p)
		if id == "" {
			id = strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		}
		items = append(items, profileListItem{ID: id, Name: name})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func getCachedFieldsForProfile(id string) []profileField {
	if id == "" {
		return []profileField{}
	}
	root, err := findRepoRoot()
	if err != nil {
		return []profileField{}
	}
	path := filepath.Join(root, "profiles", "government", id+".yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		return []profileField{}
	}
	var p profileYAMLFields
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	if err := dec.Decode(&p); err != nil {
		return []profileField{}
	}
	if strings.TrimSpace(p.Source.URL) == "" {
		return []profileField{}
	}
	resolved, err := expandEnvPlaceholders(strings.TrimSpace(p.Source.URL))
	if err != nil {
		return []profileField{}
	}
	cacheKey := id + "|" + resolved
	if resp, ok := fieldsCacheGet(cacheKey); ok {
		return resp.Fields
	}
	return []profileField{}
}

func buildCandidates(fields []profileField) candidateBuckets {
	timeC := []candidateField{}
	joinC := []candidateField{}
	metricC := []candidateField{}
	geoC := []candidateField{}

	for _, f := range fields {
		p := strings.ToLower(f.Path + " " + f.Label)
		if score := scoreTime(f, p); score > 0 {
			timeC = append(timeC, candidateField{Path: f.Path, Label: f.Label, Type: f.Type, Score: score})
		}
		if score := scoreJoin(f, p); score > 0 {
			joinC = append(joinC, candidateField{Path: f.Path, Label: f.Label, Type: f.Type, Score: score})
		}
		if score := scoreMetric(f, p); score > 0 {
			metricC = append(metricC, candidateField{Path: f.Path, Label: f.Label, Type: f.Type, Score: score})
		}
		if score := scoreGeo(f, p); score > 0 {
			geoC = append(geoC, candidateField{Path: f.Path, Label: f.Label, Type: f.Type, Score: score})
		}
	}

	sortCandidates(timeC)
	sortCandidates(joinC)
	sortCandidates(metricC)
	sortCandidates(geoC)

	return candidateBuckets{Time: timeC, Join: joinC, Metric: metricC, Geo: geoC}
}

func sortCandidates(in []candidateField) {
	sort.Slice(in, func(i, j int) bool {
		if in[i].Score != in[j].Score {
			return in[i].Score > in[j].Score
		}
		return in[i].Path < in[j].Path
	})
}

func scoreTime(f profileField, p string) int {
	score := 0
	if strings.Contains(p, "year") || strings.Contains(p, "date") || strings.Contains(p, "time") || strings.Contains(p, "timestamp") {
		score += 3
	}
	if f.Type == "number" {
		if n, ok := asNumber(f.Sample); ok {
			if n >= 1900 && n <= 2100 {
				score += 2
			}
		}
	}
	if f.Type == "string" {
		if strings.Contains(p, "timestamp") || strings.Contains(p, "date") || strings.Contains(p, "time") {
			score += 2
		}
	}
	return score
}

func scoreJoin(f profileField, p string) int {
	if f.Type != "string" {
		return 0
	}
	score := 1
	if strings.Contains(p, "id") || strings.Contains(p, "code") || strings.Contains(p, "cik") || strings.Contains(p, "fips") || strings.Contains(p, "state") || strings.Contains(p, "series") {
		score += 2
	}
	return score
}

func scoreMetric(f profileField, p string) int {
	if f.Type != "number" {
		return 0
	}
	score := 1
	if strings.Contains(p, "rate") || strings.Contains(p, "count") || strings.Contains(p, "total") || strings.Contains(p, "amount") || strings.Contains(p, "value") {
		score += 2
	}
	return score
}

func scoreGeo(f profileField, p string) int {
	if !strings.Contains(p, "state") && !strings.Contains(p, "fips") && !strings.Contains(p, "lat") && !strings.Contains(p, "lon") && !strings.Contains(p, "zip") && !strings.Contains(p, "county") && !strings.Contains(p, "census") {
		return 0
	}
	score := 1
	if strings.Contains(p, "state") || strings.Contains(p, "fips") || strings.Contains(p, "county") || strings.Contains(p, "census") {
		score += 2
	}
	return score
}

func asNumber(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case int32:
		return float64(t), true
	case json.Number:
		n, err := t.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
