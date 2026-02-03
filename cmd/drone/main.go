package main

import (
	"bytes"
	"context"
	"crypto/rand"
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

type registerRequest struct {
	ID string `json:"id"`
}

type registerResponse struct {
	ID               string   `json:"id"`
	Status           string   `json:"status"`
	AssignedProfiles []string `json:"assigned_profiles"`
}

type heartbeatRequest struct {
	ID string `json:"id"`
}

type profileEnvelope struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Content string `json:"content"`
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

	// Shutdown handling
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logLine("WARN", droneID, "shutdown_signal_received")
		cancel()
	}()

	// Register
	var regResp registerResponse
	if err := doJSON(ctx, client, http.MethodPost, controlPlane+"/api/drones/register",
		map[string]any{"id": droneID}, &regResp); err != nil {
		logLine("ERROR", droneID, "register_failed err=%s", err.Error())
		os.Exit(1)
	}

	profiles := regResp.AssignedProfiles
	logLine("INFO", droneID, "registered profiles_assigned=%d", len(profiles))

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately once, then on ticker.
	if err := iteration(ctx, client, controlPlane, droneID, profiles); err != nil {
		logLine("WARN", droneID, "iteration_completed_with_errors err=%s", err.Error())
	}

	for {
		select {
		case <-ctx.Done():
			logLine("INFO", droneID, "shutdown_complete")
			return
		case <-ticker.C:
			if err := iteration(ctx, client, controlPlane, droneID, profiles); err != nil {
				logLine("WARN", droneID, "iteration_completed_with_errors err=%s", err.Error())
			}
		}
	}
}

func iteration(ctx context.Context, client *http.Client, cp, droneID string, assigned []string) error {
	var iterErr error
	processed := 0

	for _, pid := range assigned {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Fetch profile
		var env profileEnvelope
		if err := doJSON(ctx, client, http.MethodGet, cp+"/api/profiles/"+pid, nil, &env); err != nil {
			iterErr = joinErr(iterErr, fmt.Errorf("profile_get_failed id=%s err=%w", pid, err))
			continue
		}

		// YAML decode content into Profile
		var p Profile
		if err := yaml.Unmarshal([]byte(env.Content), &p); err != nil {
			iterErr = joinErr(iterErr, fmt.Errorf("profile_yaml_decode_failed id=%s err=%w", pid, err))
			continue
		}

		results, err := ProcessProfile(p)
		if err != nil {
			// Contract: log error, do not panic; continue
			iterErr = joinErr(iterErr, fmt.Errorf("process_failed id=%s err=%w", pid, err))
			continue
		}

		// Send results
		payload := map[string]any{
			"drone_id":   droneID,
			"profile_id": pid,
			"data":       results,
		}
		var resp any
		if err := doJSON(ctx, client, http.MethodPost, cp+"/api/results", payload, &resp); err != nil {
			iterErr = joinErr(iterErr, fmt.Errorf("results_post_failed id=%s err=%w", pid, err))
			continue
		}
		processed++
	}

	// Heartbeat
	var hbResp any
	if err := doJSON(ctx, client, http.MethodPost, cp+"/api/drones/heartbeat", map[string]any{"id": droneID}, &hbResp); err != nil {
		iterErr = joinErr(iterErr, fmt.Errorf("heartbeat_failed err=%w", err))
	}

	logLine("INFO", droneID, "processed_profiles=%d heartbeat=sent", processed)
	return iterErr
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
