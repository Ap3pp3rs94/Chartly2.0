package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type enqueueReq struct {
	SourceID    string `json:"source_id"`
	JobType     string `json:"job_type"`
	RequestedAt string `json:"requested_at"`
}

type Job struct {
	JobID       string `json:"job_id"`
	TenantID    string `json:"tenant_id"`
	SourceID    string `json:"source_id"`
	JobType     string `json:"job_type"`
	Status      string `json:"status"`
	RequestedAt string `json:"requested_at"`
	EnqueuedAt  string `json:"enqueued_at"`
}

type enqueueResp struct {
	Job Job `json:"job"`
}

// v0 wiring: local in-memory queue. Orchestrator integration will replace this.
// Keep bounded and non-blocking.
var IngestQueue = make(chan Job, 10_000)

func genJobID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "job_" + hex.EncodeToString(b[:]), nil
}

func parseOrNow(s string) (string, bool) {
	if strings.TrimSpace(s) == "" {
		return time.Now().UTC().Format(time.RFC3339Nano), true
	}

	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		// allow RFC3339Nano too
		t, err2 := time.Parse(time.RFC3339Nano, s)
		if err2 != nil {
			return "", false
		}
		return t.UTC().Format(time.RFC3339Nano), true
	}

	return t.UTC().Format(time.RFC3339Nano), true
}

// IngestionEnqueue handles POST /sources/enqueue
func IngestionEnqueue(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromHeader(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "missing_tenant", "X-Tenant-Id header is required")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer r.Body.Close()

	var req enqueueReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}

	req.SourceID = strings.TrimSpace(req.SourceID)
	req.JobType = strings.TrimSpace(req.JobType)

	if req.SourceID == "" {
		writeErr(w, http.StatusBadRequest, "missing_source_id", "source_id is required")
		return
	}

	if req.JobType == "" {
		req.JobType = "ingest"
	}
	if req.JobType != "ingest" {
		writeErr(w, http.StatusBadRequest, "invalid_job_type", "job_type must be 'ingest' for v0")
		return
	}

	// Validate source exists and is enabled (tenant-scoped) using the in-memory store from sources.go.
	list := sources.list(tenantID)
	var src *Source
	for i := range list {
		if list[i].ID == req.SourceID {
			src = &list[i]
			break
		}
	}
	if src == nil {
		writeErr(w, http.StatusNotFound, "source_not_found", "source not found")
		return
	}
	if !src.Enabled {
		writeErr(w, http.StatusConflict, "source_disabled", "source is disabled")
		return
	}

	jobID, err := genJobID()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "id_generation_failed", "failed to generate job id")
		return
	}

	requestedAt, ok := parseOrNow(req.RequestedAt)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid_requested_at", "requested_at must be RFC3339")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	job := Job{
		JobID:       jobID,
		TenantID:    tenantID,
		SourceID:    req.SourceID,
		JobType:     "ingest",
		Status:      "queued",
		RequestedAt: requestedAt,
		EnqueuedAt:  now,
	}

	// Non-blocking enqueue
	select {
	case IngestQueue <- job:
		writeJSON(w, http.StatusAccepted, enqueueResp{Job: job})
	default:
		writeErr(w, http.StatusServiceUnavailable, "queue_full", "ingest queue is full")
		return
	}
}
