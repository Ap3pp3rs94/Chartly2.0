package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type profilesListResp struct {
	Profiles []profileListItem `json:"profiles"`
}

type profileListItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type profileYAML struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
}

// ListProfiles handles GET /api/profiles
func ListProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	root, err := findRepoRoot()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "repo_root_not_found", "repo root not found")
		return
	}
	dir := filepath.Join(root, "profiles", "government")
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list_failed", "failed to list profiles")
		return
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

	writeJSON(w, http.StatusOK, profilesListResp{Profiles: items})
}

func loadProfileMeta(path string) (string, string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	var p profileYAML
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	if err := dec.Decode(&p); err != nil {
		return "", ""
	}
	id := strings.TrimSpace(p.ID)
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = id
	}
	return id, name
}

func profilesDir() string {
	root, err := findRepoRoot()
	if err != nil {
		return filepath.FromSlash("profiles/government")
	}
	return filepath.Join(root, "profiles", "government")
}
