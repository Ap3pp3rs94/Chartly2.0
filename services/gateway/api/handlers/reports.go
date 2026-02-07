package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type reportFile struct {
	ReportID  string `json:"report_id"`
	Name      string `json:"name"`
	PlanHash  string `json:"plan_hash"`
	CreatedAt string `json:"created_at"`
	Plan      plan   `json:"plan"`
}

type reportListResp struct {
	Reports []reportListItem `json:"reports"`
}

type reportListItem struct {
	ReportID  string `json:"report_id"`
	Name      string `json:"name"`
	PlanHash  string `json:"plan_hash"`
	CreatedAt string `json:"created_at"`
}

type createReportReq struct {
	Name string `json:"name"`
	Plan plan   `json:"plan"`
}

type createReportResp struct {
	ReportID  string `json:"report_id"`
	Name      string `json:"name"`
	PlanHash  string `json:"plan_hash"`
	CreatedAt string `json:"created_at"`
}

type runReportResp struct {
	RunID    string            `json:"run_id"`
	Status   string            `json:"status"`
	Next     string            `json:"next"`
	Profiles []profileListItem `json:"profiles"`
}

// ListReports handles GET /api/reports
func ListReports(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	reports, _ := loadReports()
	items := make([]reportListItem, 0, len(reports))
	for _, r := range reports {
		items = append(items, reportListItem{
			ReportID:  r.ReportID,
			Name:      r.Name,
			PlanHash:  r.PlanHash,
			CreatedAt: r.CreatedAt,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ReportID < items[j].ReportID })
	writeJSON(w, http.StatusOK, reportListResp{Reports: items})
}

// CreateReport handles POST /api/reports
func CreateReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if !requireAPIKey(w, r) {
		return
	}

	var req createReportReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "missing_name", "name is required")
		return
	}

	planHash := hashPlan(req.Plan)
	short := planHash
	if len(short) > 12 {
		short = short[:12]
	}
	reportID := "rep_" + short
	createdAt := time.Now().UTC().Format(time.RFC3339)

	rep := reportFile{
		ReportID:  reportID,
		Name:      name,
		PlanHash:  planHash,
		CreatedAt: createdAt,
		Plan:      req.Plan,
	}

	if err := saveReport(rep); err != nil {
		writeErr(w, http.StatusInternalServerError, "write_failed", "failed to save report")
		return
	}

	writeJSON(w, http.StatusCreated, createReportResp{
		ReportID:  reportID,
		Name:      name,
		PlanHash:  planHash,
		CreatedAt: createdAt,
	})
}

// GetReport handles GET /api/reports/{id}
func GetReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/reports/")
	id = strings.Trim(id, "/")
	if id == "" || strings.Contains(id, "..") {
		writeErr(w, http.StatusBadRequest, "invalid_id", "invalid report id")
		return
	}

	rep, err := loadReportByID(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "report not found")
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// RunReport handles POST /api/reports/{id}:run
func RunReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/reports/")
	if !strings.HasSuffix(id, ":run") {
		writeErr(w, http.StatusNotFound, "not_found", "endpoint not found")
		return
	}
	id = strings.TrimSuffix(id, ":run")
	id = strings.Trim(id, "/")
	if id == "" || strings.Contains(id, "..") {
		writeErr(w, http.StatusBadRequest, "invalid_id", "invalid report id")
		return
	}

	rep, err := loadReportByID(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "report not found")
		return
	}

	runID := "run_" + strings.TrimPrefix(rep.ReportID, "rep_")
	resp := runReportResp{
		RunID:    runID,
		Status:   "planned",
		Next:     "fetch_results",
		Profiles: rep.Plan.Profiles,
	}
	writeJSON(w, http.StatusOK, resp)
}

func requireAPIKey(w http.ResponseWriter, r *http.Request) bool {
	expected := strings.TrimSpace(os.Getenv("REGISTRY_API_KEY"))
	if expected == "" {
		writeErr(w, http.StatusForbidden, "api_key_not_configured", "api key not configured")
		return false
	}
	provided := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if provided == "" || provided != expected {
		writeErr(w, http.StatusForbidden, "forbidden", "invalid api key")
		return false
	}
	return true
}

func reportsDir() (string, error) {
	root, err := findRepoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "reports"), nil
}

func saveReport(r reportFile) error {
	dir, err := reportsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	filename := r.ReportID + ".json"
	path := filepath.Join(dir, filename)
	b, _ := json.MarshalIndent(r, "", "  ")
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

func loadReports() ([]reportFile, error) {
	dir, err := reportsDir()
	if err != nil {
		return nil, err
	}
	files, err := filepath.Glob(filepath.Join(dir, "rep_*.json"))
	if err != nil {
		return nil, err
	}
	out := make([]reportFile, 0, len(files))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var r reportFile
		if err := json.Unmarshal(b, &r); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func loadReportByID(id string) (reportFile, error) {
	dir, err := reportsDir()
	if err != nil {
		return reportFile{}, err
	}
	path := filepath.Join(dir, id+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		return reportFile{}, err
	}
	var r reportFile
	if err := json.Unmarshal(b, &r); err != nil {
		return reportFile{}, err
	}
	return r, nil
}
