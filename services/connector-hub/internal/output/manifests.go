package output

import (

	"encoding/json"

	"errors"

	"os"

	"path/filepath"

	"sort"

	"strings"

	"sync"

	"time"
)

type LoggerFn func(level, msg string, fields map[string]any)

type Manifest struct {

	Version      string            `json:"version"`

	CreatedAt    string            `json:"created_at"`

	TenantID     string            `json:"tenant_id"`

	SourceID     string            `json:"source_id"`

	ConnectorID  string            `json:"connector_id"`

	JobID        string            `json:"job_id"`

	Kind         string            `json:"kind"`

	Paths        []string          `json:"paths"`

	BytesWritten int64             `json:"bytes_written"`

	Checksums    map[string]string `json:"checksums,omitempty"` // path -> sha256 (also mirrored via ChecksumsList)

	ChecksumsList []ChecksumEntry  `json:"checksums_list,omitempty"`

	Notes        map[string]string `json:"notes,omitempty"`
}

type ChecksumEntry struct {

	Path   string `json:"path"`

	SHA256 string `json:"sha256"`
}

type ManifestWriter struct {

	baseDir string

	logger  LoggerFn


	mu   sync.Mutex

	work map[string]*Manifest
}

func NewManifestWriter(baseDir string) *ManifestWriter {

	if strings.TrimSpace(baseDir) == "" {


		baseDir = "./data"

	}

	return &ManifestWriter{


		baseDir: baseDir,


		logger:  func(string, string, map[string]any) {},


		work:    make(map[string]*Manifest),

	}
}

func (w *ManifestWriter) WithLogger(fn LoggerFn) *ManifestWriter {

	if fn != nil {


		w.logger = fn

	}

	return w
}

func (w *ManifestWriter) Begin(jobID, tenant, source, connector, kind string) error {

	jobID = strings.TrimSpace(jobID)

	if jobID == "" {


		return errors.New("jobID empty")

	}


	w.mu.Lock()
	defer w.mu.Unlock()


	if _, ok := w.work[jobID]; ok {


		return errors.New("manifest already begun")

	}

	if strings.TrimSpace(kind) == "" {


		kind = "raw"

	}


	w.work[jobID] = &Manifest{


		Version:     "v1",


		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),


		TenantID:    strings.TrimSpace(tenant),


		SourceID:    strings.TrimSpace(source),


		ConnectorID: strings.TrimSpace(connector),


		JobID:       jobID,


		Kind:        kind,


		Paths:       []string{},


		Checksums:   make(map[string]string),


		Notes:       make(map[string]string),

	}

	return nil
}

func (w *ManifestWriter) AddPath(jobID, path string, bytes int64, sha256 string) error {

	jobID = strings.TrimSpace(jobID)

	path = strings.TrimSpace(path)

	if jobID == "" || path == "" {


		return errors.New("jobID/path empty")

	}


	w.mu.Lock()
	defer w.mu.Unlock()


	m := w.work[jobID]

	if m == nil {


		return errors.New("manifest not begun")

	}


	m.Paths = append(m.Paths, path)

	m.BytesWritten += bytes

	if strings.TrimSpace(sha256) != "" {


		m.Checksums[path] = strings.TrimSpace(sha256)

	}

	return nil
}

func (w *ManifestWriter) Note(jobID, k, v string) error {

	jobID = strings.TrimSpace(jobID)

	k = strings.TrimSpace(k)

	if jobID == "" || k == "" {


		return errors.New("jobID/key empty")

	}


	w.mu.Lock()
	defer w.mu.Unlock()


	m := w.work[jobID]

	if m == nil {


		return errors.New("manifest not begun")

	}

	m.Notes[k] = strings.TrimSpace(v)

	return nil
}

func (w *ManifestWriter) Commit(jobID string) (string, error) {

	jobID = strings.TrimSpace(jobID)

	if jobID == "" {


		return "", errors.New("jobID empty")

	}


	w.mu.Lock()
	m := w.work[jobID]
	if m == nil {


		w.mu.Unlock()


		return "", errors.New("manifest not begun")

	}

	// detach for immutable commit
	delete(w.work, jobID)
	w.mu.Unlock()


	// determinism: sort paths
	sort.Strings(m.Paths)


	// deterministic checksum list
	keys := make([]string, 0, len(m.Checksums))

	for k := range m.Checksums {


		keys = append(keys, k)

	}

	sort.Strings(keys)

	m.ChecksumsList = make([]ChecksumEntry, 0, len(keys))

	for _, k := range keys {


		m.ChecksumsList = append(m.ChecksumsList, ChecksumEntry{Path: k, SHA256: m.Checksums[k]})

	}


	tenant := sanitizeSeg(m.TenantID, "tenant")

	source := sanitizeSeg(m.SourceID, "source")

	conn := sanitizeSeg(m.ConnectorID, "connector")

	outPath := filepath.Join(w.baseDir, tenant, source, conn, "manifests", jobID+".json")


	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {


		return "", err

	}


	// refuse overwrite
	if _, err := os.Stat(outPath); err == nil {


		return "", errors.New("manifest already exists")

	}


	tmp := outPath + ".tmp." + strconvNano()

	b, err := json.MarshalIndent(m, "", "  ")

	if err != nil {


		return "", err

	}


	if err := os.WriteFile(tmp, b, 0o644); err != nil {


		return "", err

	}


	if err := os.Rename(tmp, outPath); err != nil {


		_ = os.Remove(tmp)


		return "", err

	}


	w.logger("info", "manifest_committed", map[string]any{


		"event":  "manifest_committed",


		"path":   outPath,


		"job_id": m.JobID,

	})


	return outPath, nil
}

func (w *ManifestWriter) Abort(jobID string) {

	jobID = strings.TrimSpace(jobID)

	if jobID == "" {


		return

	}

	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.work, jobID)
}

func strconvNano() string {

	return time.Now().UTC().Format("20060102T150405.000000000Z")
}

func sanitizeSeg(s, def string) string {

	s = strings.TrimSpace(s)

	if s == "" {


		return def

	}

	s = strings.ToLower(s)

	s = strings.ReplaceAll(s, "..", "")

	s = strings.ReplaceAll(s, "/", "_")

	s = strings.ReplaceAll(s, "\\", "_")

	s = strings.ReplaceAll(s, ":", "_")

	return s
}
