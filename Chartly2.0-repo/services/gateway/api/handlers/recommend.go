package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const recommendCacheTTL = 5 * time.Minute

type recommendReq struct {
	Intent      string               `json:"intent"`
	Profiles    []string             `json:"profiles"`
	Preferences recommendPreferences `json:"preferences"`
}

type recommendPreferences struct {
	GeoLevel         string `json:"geo_level"`
	TimeGranularity  string `json:"time_granularity"`
	MetricPreference string `json:"metric_preference"`
}

type plan struct {
	Intent      string               `json:"intent"`
	ReportType  string               `json:"report_type"`
	Profiles    []profileListItem    `json:"profiles"`
	JoinKey     *planField           `json:"join_key,omitempty"`
	X           *planField           `json:"x,omitempty"`
	Y           *planField           `json:"y,omitempty"`
	Time        *planField           `json:"time,omitempty"`
	Preferences recommendPreferences `json:"preferences,omitempty"`
}

type planField struct {
	ProfileID  string  `json:"profile_id,omitempty"`
	Path       string  `json:"path"`
	Label      string  `json:"label"`
	Type       string  `json:"type"`
	Confidence float64 `json:"confidence"`
}

type recommendResp struct {
	Plan       plan     `json:"plan"`
	PlanHash   string   `json:"plan_hash"`
	Confidence float64  `json:"confidence"`
	Why        []string `json:"why"`
	Fallbacks  []string `json:"fallbacks,omitempty"`
}

type recommendCacheEntry struct {
	resp      recommendResp
	expiresAt time.Time
}

var (
	recommendMu    sync.Mutex
	recommendCache = map[string]recommendCacheEntry{}
)

// PostRecommendations handles POST /api/recommendations
func PostRecommendations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	var req recommendReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}

	req.Intent = strings.ToLower(strings.TrimSpace(req.Intent))
	if req.Intent == "" {
		req.Intent = "compare"
	}

	canon, _ := json.Marshal(req)
	key := sha256Hex(canon)
	if resp, ok := recommendCacheGet(key); ok {
		etag := resp.PlanHash
		if inm := strings.TrimSpace(r.Header.Get("If-None-Match")); inm != "" && inm == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		writeJSON(w, http.StatusOK, resp)
		return
	}

	catalog, _ := getCatalogCached()
	resp := buildRecommendation(req, catalog)
	etag := resp.PlanHash
	recommendCacheSet(key, resp)
	if inm := strings.TrimSpace(r.Header.Get("If-None-Match")); inm != "" && inm == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)
	writeJSON(w, http.StatusOK, resp)
}

func buildRecommendation(req recommendReq, cat catalogResp) recommendResp {
	profiles := cat.Profiles
	profileMap := map[string]catalogProfile{}
	for _, p := range profiles {
		profileMap[p.ID] = p
	}

	selected := selectProfiles(req, profiles)
	selectedItems := make([]profileListItem, 0, len(selected))
	for _, id := range selected {
		p := profileMap[id]
		selectedItems = append(selectedItems, profileListItem{ID: id, Name: p.Name})
	}

	intent := req.Intent
	p := plan{Intent: intent, Profiles: selectedItems, Preferences: req.Preferences}
	resp := recommendResp{Plan: p, Confidence: 0.2, Why: []string{}}

	prefMetric := strings.ToLower(strings.TrimSpace(req.Preferences.MetricPreference))
	prefGeo := strings.ToLower(strings.TrimSpace(req.Preferences.GeoLevel))
	prefTime := strings.ToLower(strings.TrimSpace(req.Preferences.TimeGranularity))

	switch intent {
	case "compare":
		resp = recommendCompare(resp, selected, profileMap, prefMetric, prefGeo)
	case "trend":
		resp = recommendTrend(resp, selected, profileMap, prefMetric, prefTime)
	case "rank":
		resp = recommendRank(resp, selected, profileMap, prefMetric, prefGeo)
	case "explain", "anomaly":
		resp.Plan.ReportType = "table_preview"
		resp.Why = append(resp.Why, "intent is planned; returning table preview")
		resp.Confidence = 0.1
	default:
		resp.Plan.ReportType = "table_preview"
		resp.Why = append(resp.Why, "unknown intent; returning table preview")
		resp.Confidence = 0.1
	}

	if resp.Plan.ReportType == "" {
		resp.Plan.ReportType = "table_preview"
	}

	resp.PlanHash = hashPlan(resp.Plan)
	return resp
}

func hashPlan(p plan) string {
	b, _ := json.Marshal(p)
	return sha256Hex(b)
}

func recommendCompare(resp recommendResp, selected []string, profileMap map[string]catalogProfile, prefMetric, prefGeo string) recommendResp {
	if len(selected) < 2 {
		resp.Plan.ReportType = "table_preview"
		resp.Why = append(resp.Why, "need two profiles for compare; using table preview")
		return resp
	}
	pA := profileMap[selected[0]]
	pB := profileMap[selected[1]]

	metricA := pickMetric(pA.Candidates.Metric, prefMetric)
	metricB := pickMetric(pB.Candidates.Metric, prefMetric)
	join := pickSharedJoin(pA.Candidates.Join, pB.Candidates.Join, prefGeo)

	if metricA != nil && metricB != nil && join != nil {
		resp.Plan.ReportType = "correlation_scatter"
		resp.Plan.X = toPlanField(selected[0], metricA, 0.7)
		resp.Plan.Y = toPlanField(selected[1], metricB, 0.7)
		resp.Plan.JoinKey = toPlanField("", join, 0.7)
		resp.Confidence = 0.7
		resp.Why = append(resp.Why, "both profiles have numeric metrics and a shared join key")
		return resp
	}

	resp.Plan.ReportType = "table_preview"
	resp.Why = append(resp.Why, "missing shared join key or numeric metric; using table preview")
	resp.Confidence = 0.2
	return resp
}

func recommendTrend(resp recommendResp, selected []string, profileMap map[string]catalogProfile, prefMetric, prefTime string) recommendResp {
	if len(selected) < 1 {
		resp.Plan.ReportType = "table_preview"
		resp.Why = append(resp.Why, "no profiles available")
		return resp
	}
	p := profileMap[selected[0]]
	timeF := pickTime(p.Candidates.Time, prefTime)
	metric := pickMetric(p.Candidates.Metric, prefMetric)
	if timeF != nil && metric != nil {
		resp.Plan.ReportType = "time_series_line"
		resp.Plan.Time = toPlanField(selected[0], timeF, 0.7)
		resp.Plan.Y = toPlanField(selected[0], metric, 0.7)
		resp.Confidence = 0.65
		resp.Why = append(resp.Why, "profile has time and metric candidates")
		return resp
	}
	resp.Plan.ReportType = "table_preview"
	resp.Why = append(resp.Why, "missing time or metric candidates")
	resp.Confidence = 0.2
	return resp
}

func recommendRank(resp recommendResp, selected []string, profileMap map[string]catalogProfile, prefMetric, prefGeo string) recommendResp {
	if len(selected) < 1 {
		resp.Plan.ReportType = "table_preview"
		resp.Why = append(resp.Why, "no profiles available")
		return resp
	}
	p := profileMap[selected[0]]
	join := pickJoin(p.Candidates.Join, prefGeo)
	metric := pickMetric(p.Candidates.Metric, prefMetric)
	if join != nil && metric != nil {
		resp.Plan.ReportType = "categorical_bar"
		resp.Plan.JoinKey = toPlanField("", join, 0.7)
		resp.Plan.Y = toPlanField(selected[0], metric, 0.7)
		resp.Confidence = 0.6
		resp.Why = append(resp.Why, "profile has join key and metric candidates")
		return resp
	}
	resp.Plan.ReportType = "table_preview"
	resp.Why = append(resp.Why, "missing join key or metric")
	resp.Confidence = 0.2
	return resp
}

func selectProfiles(req recommendReq, profiles []catalogProfile) []string {
	ids := make([]string, 0, len(req.Profiles))
	seen := map[string]bool{}
	for _, id := range req.Profiles {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) > 0 {
		return ids
	}
	out := make([]string, 0, 2)
	for _, p := range profiles {
		out = append(out, p.ID)
		if len(out) >= 2 {
			break
		}
	}
	return out
}

func pickMetric(cands []candidateField, pref string) *candidateField {
	if len(cands) == 0 {
		return nil
	}
	if pref != "" && pref != "auto" {
		for _, c := range cands {
			p := strings.ToLower(c.Path + " " + c.Label)
			if strings.Contains(p, pref) {
				c2 := c
				return &c2
			}
		}
	}
	c := cands[0]
	return &c
}

func pickTime(cands []candidateField, pref string) *candidateField {
	if len(cands) == 0 {
		return nil
	}
	if pref != "" && pref != "auto" {
		for _, c := range cands {
			p := strings.ToLower(c.Path + " " + c.Label)
			if strings.Contains(p, pref) {
				c2 := c
				return &c2
			}
		}
	}
	c := cands[0]
	return &c
}

func pickJoin(cands []candidateField, pref string) *candidateField {
	if len(cands) == 0 {
		return nil
	}
	if pref != "" && pref != "auto" && pref != "none" {
		for _, c := range cands {
			p := strings.ToLower(c.Path + " " + c.Label)
			if strings.Contains(p, pref) {
				c2 := c
				return &c2
			}
		}
	}
	c := cands[0]
	return &c
}

func pickSharedJoin(a, b []candidateField, pref string) *candidateField {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	bmap := map[string]candidateField{}
	for _, c := range b {
		bmap[c.Path] = c
	}
	shared := make([]candidateField, 0)
	for _, c := range a {
		if _, ok := bmap[c.Path]; ok {
			shared = append(shared, c)
		}
	}
	if len(shared) == 0 {
		return nil
	}
	if pref != "" && pref != "auto" && pref != "none" {
		for _, c := range shared {
			p := strings.ToLower(c.Path + " " + c.Label)
			if strings.Contains(p, pref) {
				c2 := c
				return &c2
			}
		}
	}
	sort.Slice(shared, func(i, j int) bool { return shared[i].Path < shared[j].Path })
	c := shared[0]
	return &c
}

func toPlanField(profileID string, c *candidateField, conf float64) *planField {
	if c == nil {
		return nil
	}
	return &planField{ProfileID: profileID, Path: c.Path, Label: c.Label, Type: c.Type, Confidence: conf}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func recommendCacheGet(key string) (recommendResp, bool) {
	recommendMu.Lock()
	defer recommendMu.Unlock()
	if e, ok := recommendCache[key]; ok {
		if time.Now().Before(e.expiresAt) {
			return e.resp, true
		}
		delete(recommendCache, key)
	}
	return recommendResp{}, false
}

func recommendCacheSet(key string, resp recommendResp) {
	recommendMu.Lock()
	defer recommendMu.Unlock()
	recommendCache[key] = recommendCacheEntry{resp: resp, expiresAt: time.Now().Add(recommendCacheTTL)}
}
