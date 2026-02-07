package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type profileSchemaResp struct {
	ProfileID     string         `json:"profile_id"`
	Name          string         `json:"name"`
	Fields        []profileField `json:"fields"`
	JoinKeys      []string       `json:"join_keys"`
	NumericFields []string       `json:"numeric_fields"`
}

type profileField struct {
	Path    string      `json:"path"`
	Type    string      `json:"type"`
	Example interface{} `json:"example"`
}

type profileYAML struct {
	ID      string            `yaml:"id"`
	Name    string            `yaml:"name"`
	Mapping map[string]string `yaml:"mapping"`
}

// GetProfileSchema handles GET /api/profiles/{id}/schema
func GetProfileSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	// expected path: /api/profiles/{id}/schema
	path := strings.TrimPrefix(r.URL.Path, "/api/profiles/")
	if !strings.HasSuffix(path, "/schema") {
		writeErr(w, http.StatusNotFound, "not_found", "endpoint not found")
		return
	}
	id := strings.TrimSuffix(path, "/schema")
	id = strings.Trim(id, "/")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "invalid_profile_id", "profile_id is required")
		return
	}
	if strings.Contains(id, "..") {
		writeErr(w, http.StatusBadRequest, "invalid_profile_id", "invalid profile id")
		return
	}

	file := filepath.Join(profilesDir(), id+".yaml")
	b, err := os.ReadFile(file)
	if err != nil {
		writeErr(w, http.StatusNotFound, "profile_not_found", "profile not found")
		return
	}

	p, err := parseProfileYAML(b)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_profile", "invalid profile yaml")
		return
	}

	fields, joins, nums := deriveFields(p.Mapping)

	resp := profileSchemaResp{
		ProfileID:     firstNonEmpty(p.ID, id),
		Name:          firstNonEmpty(p.Name, id),
		Fields:        fields,
		JoinKeys:      joins,
		NumericFields: nums,
	}

	writeJSON(w, http.StatusOK, resp)
}

func parseProfileYAML(b []byte) (profileYAML, error) {
	var p profileYAML
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	if err := dec.Decode(&p); err != nil {
		return profileYAML{}, err
	}
	if p.Mapping == nil {
		p.Mapping = map[string]string{}
	}
	return p, nil
}

func deriveFields(mapping map[string]string) ([]profileField, []string, []string) {
	set := map[string]profileField{}
	for _, dst := range mapping {
		p := strings.TrimSpace(dst)
		if p == "" {
			continue
		}
		if _, ok := set[p]; ok {
			continue
		}
		typ := inferType(p)
		set[p] = profileField{Path: p, Type: typ, Example: exampleForPath(p, typ)}
	}

	fields := make([]profileField, 0, len(set))
	for _, v := range set {
		fields = append(fields, v)
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Path < fields[j].Path })

	joins := make([]string, 0)
	nums := make([]string, 0)
	for _, f := range fields {
		if f.Type == "number" {
			nums = append(nums, f.Path)
		} else {
			joins = append(joins, f.Path)
		}
	}
	sort.Strings(joins)
	sort.Strings(nums)

	return fields, joins, nums
}

func inferType(path string) string {
	p := strings.ToLower(path)
	if strings.Contains(p, "measures") || strings.Contains(p, "measure") {
		return "number"
	}
	numTokens := []string{"rate", "count", "total", "value", "amount", "price", "cost", "year", "month", "day", "pct", "percent", "ratio", "index", "score", "number"}
	for _, t := range numTokens {
		if strings.Contains(p, t) {
			return "number"
		}
	}
	return "string"
}

func exampleForPath(path, typ string) interface{} {
	p := strings.ToLower(path)
	if typ == "number" {
		switch {
		case strings.Contains(p, "year"):
			return 2024
		case strings.Contains(p, "rate"):
			return 4.2
		case strings.Contains(p, "population"):
			return 39538223
		default:
			return 1
		}
	}

	switch {
	case strings.Contains(p, "state_code"):
		return "CA"
	case strings.Contains(p, "name"):
		return "California"
	case strings.Contains(p, "cik"):
		return "0000789019"
	case strings.Contains(p, "series"):
		return "LNS14000000"
	default:
		return "example"
	}
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return strings.TrimSpace(a)
	}
	return strings.TrimSpace(b)
}

// profileDigest returns a deterministic sha256 for profile bytes
// (used internally for future proofing; not exposed here yet).
func profileDigest(b []byte) string {
	sum := sha256.Sum256(normalizeNewline(b))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizeNewline(b []byte) []byte {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return []byte(s)
}

// writeJSON and writeErr are defined in sources.go

// readAll is used to cap input reads safely (future use).
func readAll(r io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		return nil, errors.New("invalid_limit")
	}
	return io.ReadAll(io.LimitReader(r, limit))
}
