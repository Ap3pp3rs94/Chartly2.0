package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	userAgent        = "Chartly-Drone/1.0"
	httpTimeout      = 30 * time.Second
	maxBodyBytes     = 8 << 20
	defaultInterval  = 5 * time.Minute
	retryMaxAttempts = 3
)

type registerResponse struct {
	ID               string   `json:"id"`
	Status           string   `json:"status"`
	AssignedProfiles []string `json:"assigned_profiles"`
}

type profileEnvelope struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Version  string         `json:"version"`
	Content  string         `json:"content"`
	Enabled  *bool          `json:"enabled,omitempty"`
	Interval string         `json:"interval,omitempty"`
	Jitter   string         `json:"jitter,omitempty"`
	Limits   *profileLimits `json:"limits,omitempty"`
}

type profileLimits struct {
	MaxRecords *int `json:"max_records,omitempty"`
	MaxPages   *int `json:"max_pages,omitempty"`
	MaxBytes   *int `json:"max_bytes,omitempty"`
}

type workResponse struct {
	DroneID  string   `json:"drone_id"`
	Profiles []string `json:"profiles"`
}

type runReport struct {
	RunID      string `json:"run_id"`
	DroneID    string `json:"drone_id"`
	ProfileID  string `json:"profile_id"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	Status     string `json:"status"`
	RowsOut    int    `json:"rows_out"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

func main() {
	controlPlane := strings.TrimSpace(os.Getenv("CONTROL_PLANE"))
	if controlPlane == "" {
		fmt.Fprintln(os.Stderr, "missing CONTROL_PLANE")
		os.Exit(1)
	}
	controlPlane = strings.TrimRight(controlPlane, "/")

	droneID := strings.TrimSpace(os.Getenv("DRONE_ID"))
	if droneID == "" {
		droneID = mustUUIDv4()
	}

	interval := defaultInterval
	if v := strings.TrimSpace(os.Getenv("PROCESS_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			interval = d
		}
	}

	client := &http.Client{Timeout: httpTimeout}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logLine("WARN", droneID, "shutdown_signal_received")
		cancel()
	}()

	var regResp registerResponse
	if err := doJSON(ctx, client, http.MethodPost, controlPlane+"/api/drones/register",
		map[string]any{"id": droneID}, &regResp); err != nil {
		logLine("ERROR", droneID, "register_failed err=%s", err.Error())
		os.Exit(1)
	}

	assigned := regResp.AssignedProfiles
	logLine("INFO", droneID, "registered profiles_assigned=%d", len(assigned))

	lastRun := make(map[string]time.Time)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	if err := iteration(ctx, client, controlPlane, droneID, assigned, lastRun); err != nil {
		logLine("WARN", droneID, "iteration_completed_with_errors err=%s", err.Error())
	}

	for {
		select {
		case <-ctx.Done():
			logLine("INFO", droneID, "shutdown_complete")
			return
		case <-ticker.C:
			if err := iteration(ctx, client, controlPlane, droneID, assigned, lastRun); err != nil {
				logLine("WARN", droneID, "iteration_completed_with_errors err=%s", err.Error())
			}
		}
	}
}

func iteration(ctx context.Context, client *http.Client, cp, droneID string, assigned []string, lastRun map[string]time.Time) error {
	var iterErr error
	executed := 0
	skipped := 0

	forced := fetchWorkQueue(ctx, client, cp, droneID)

	for _, pid := range assigned {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		forcedRun := forced[pid]

		var env profileEnvelope
		if err := doJSON(ctx, client, http.MethodGet, cp+"/api/profiles/"+pid, nil, &env); err != nil {
			iterErr = joinErr(iterErr, fmt.Errorf("profile_get_failed id=%s err=%w", pid, err))
			continue
		}

		if env.Enabled != nil && !*env.Enabled && !forcedRun {
			skipped++
			continue
		}

		if !forcedRun {
			if due, ok := isDue(env, lastRun[pid], droneID, pid); ok && !due {
				skipped++
				continue
			}
		}

		runID := mustUUIDv4()
		started := time.Now().UTC()

		var p Profile
		if err := yaml.Unmarshal([]byte(env.Content), &p); err != nil {
			iterErr = joinErr(iterErr, fmt.Errorf("profile_yaml_decode_failed id=%s err=%w", pid, err))
			reportRun(ctx, client, cp, runID, droneID, pid, started, time.Now().UTC(), "failed", 0, time.Since(started).Milliseconds(), "invalid_profile_yaml")
			continue
		}

		results, err := ProcessProfile(p)
		if err != nil {
			iterErr = joinErr(iterErr, fmt.Errorf("process_failed id=%s err=%w", pid, err))
			reportRun(ctx, client, cp, runID, droneID, pid, started, time.Now().UTC(), "failed", 0, time.Since(started).Milliseconds(), capError(err.Error()))
			continue
		}

		payload := map[string]any{
			"drone_id":   droneID,
			"profile_id": pid,
			"run_id":     runID,
			"data":       results,
		}
		var resp any
		if err := doJSON(ctx, client, http.MethodPost, cp+"/api/results", payload, &resp); err != nil {
			iterErr = joinErr(iterErr, fmt.Errorf("results_post_failed id=%s err=%w", pid, err))
			reportRun(ctx, client, cp, runID, droneID, pid, started, time.Now().UTC(), "partial", len(results), time.Since(started).Milliseconds(), capError(err.Error()))
			continue
		}

		finished := time.Now().UTC()
		duration := finished.Sub(started).Milliseconds()
		reportRun(ctx, client, cp, runID, droneID, pid, started, finished, "succeeded", len(results), duration, "")

		lastRun[pid] = finished
		executed++
	}

	var hbResp any
	if err := doJSON(ctx, client, http.MethodPost, cp+"/api/drones/heartbeat", map[string]any{"id": droneID}, &hbResp); err != nil {
		iterErr = joinErr(iterErr, fmt.Errorf("heartbeat_failed err=%w", err))
	}

	logLine("INFO", droneID, "executed=%d skipped=%d heartbeat=sent", executed, skipped)
	return iterErr
}

func fetchWorkQueue(ctx context.Context, client *http.Client, cp, droneID string) map[string]bool {
	out := make(map[string]bool)
	var wr workResponse
	err := doJSON(ctx, client, http.MethodGet, cp+"/api/drones/"+droneID+"/work", nil, &wr)
	if err != nil {
		if strings.Contains(err.Error(), "http_error status=404") {
			return out
		}
		return out
	}
	for _, p := range wr.Profiles {
		out[strings.TrimSpace(p)] = true
	}
	return out
}

func isDue(env profileEnvelope, last time.Time, droneID, profileID string) (bool, bool) {
	if env.Interval == "" {
		return true, true
	}
	d, err := time.ParseDuration(env.Interval)
	if err != nil || d <= 0 {
		return true, true
	}

	jitter := time.Duration(0)
	if env.Jitter != "" {
		if jd, err := time.ParseDuration(env.Jitter); err == nil && jd > 0 {
			jitter = jd
		}
	}

	if last.IsZero() {
		return true, true
	}

	next := last.Add(d + deterministicJitter(droneID, profileID, jitter))
	return time.Now().UTC().After(next), true
}

func deterministicJitter(droneID, profileID string, window time.Duration) time.Duration {
	if window <= 0 {
		return 0
	}
	h := sha256.Sum256([]byte(droneID + "|" + profileID))
	v := int64(binaryToInt(h[:8]))
	if v < 0 {
		v = -v
	}
	return time.Duration(v % int64(window))
}

func binaryToInt(b []byte) int64 {
	var v int64
	for i := 0; i < len(b); i++ {
		v = (v << 8) | int64(b[i])
	}
	return v
}

func reportRun(ctx context.Context, client *http.Client, cp, runID, droneID, profileID string, started, finished time.Time, status string, rows int, durationMs int64, errMsg string) {
	r := runReport{
		RunID:      runID,
		DroneID:    droneID,
		ProfileID:  profileID,
		StartedAt:  started.Format(time.RFC3339),
		FinishedAt: finished.Format(time.RFC3339),
		Status:     status,
		RowsOut:    rows,
		DurationMs: durationMs,
		Error:      capError(errMsg),
	}
	var resp any
	_ = doJSON(ctx, client, http.MethodPost, cp+"/api/runs", r, &resp)
}

func capError(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 2048 {
		return s[:2048]
	}
	return s
}

func doJSON(ctx context.Context, client *http.Client, method, url string, body any, out any) error {
	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyBytes = b
	}

	var lastErr error
	backoff := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

	for attempt := 1; attempt <= retryMaxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(bodyBytes))
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", userAgent)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < retryMaxAttempts {
				sleepWithContext(ctx, backoff[attempt-1])
				continue
			}
			return lastErr
		}

		b, rerr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		_ = resp.Body.Close()
		if rerr != nil {
			lastErr = rerr
			if attempt < retryMaxAttempts {
				sleepWithContext(ctx, backoff[attempt-1])
				continue
			}
			return lastErr
		}

		if resp.StatusCode >= 500 && attempt < retryMaxAttempts {
			lastErr = fmt.Errorf("server_error status=%d", resp.StatusCode)
			sleepWithContext(ctx, backoff[attempt-1])
			continue
		}
		if resp.StatusCode/100 != 2 {
			return fmt.Errorf("http_error status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(b)))
		}

		if out != nil {
			if err := json.Unmarshal(b, out); err != nil {
				return err
			}
		}
		return nil
	}

	return lastErr
}

func sleepWithContext(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return
	case <-t.C:
		return
	}
}

func mustUUIDv4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	s := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", s[0:8], s[8:12], s[12:16], s[16:20], s[20:32])
}

func logLine(level, droneID, format string, args ...any) {
	ts := time.Now().UTC().Format(time.RFC3339)
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("%s %s drone_id=%s %s\n", ts, level, droneID, msg)
}

func joinErr(a, b error) error {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return errors.Join(a, b)
}
