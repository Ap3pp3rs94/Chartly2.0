package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

// Tool version (contract-visible).
const ToolVersion = "0.1.0"

// Exit codes (tools/README.md contract-aligned).
const (
	ExitSuccess          = 0
	ExitGeneralError     = 1
	ExitInvalidArgs      = 2
	ExitPreconditionFail = 3
	ExitValidationFail   = 4
	ExitAccessDenied     = 5
)

// Config represents parsed CLI input.
// All fields are explicit; no inference is allowed.
type Config struct {
	Env         string
	TargetType  string
	TargetID    string
	WindowStart time.Time
	WindowEnd   time.Time
	Format      string
	DryRun      bool

	CompareTo      string
	Precision      string
	IncludeMetrics []string
}

// Summary is a deterministic summary of profiling results.
type Summary struct {
	TotalDurationMs int64  `json:"total_duration_ms"`
	Bottleneck      string `json:"bottleneck"`
	Regression      string `json:"regression"`
}

// BreakdownEntry represents a single contributor.
// Percent is represented deterministically as basis points (1/100 of a percent).
type BreakdownEntry struct {
	Component   string `json:"component"`
	DurationMs  int64  `json:"duration_ms"`
	PctBasisPts int64  `json:"pct_bp"` // 10000 = 100.00%
}

// Header echoes request context deterministically.
type Header struct {
	ToolVersion   string   `json:"tool_version"`
	Environment   string   `json:"environment"`
	TargetType    string   `json:"target_type"`
	TargetID      string   `json:"target_id"`
	WindowStart   string   `json:"window_start"`
	WindowEnd     string   `json:"window_end"`
	DryRun        bool     `json:"dry_run"`
	CompareTo     string   `json:"compare_to,omitempty"`
	Precision     string   `json:"precision"`
	IncludeMetric []string `json:"include_metrics,omitempty"`
}

// Output is the stable, structured profiler output.
type Output struct {
	Header    Header           `json:"header"`
	Summary   Summary          `json:"summary"`
	Breakdown []BreakdownEntry `json:"breakdown"`
}

func summaryLine(status string, code int, dur time.Duration) string {
	// Always include duration in ms for automation.
	return fmt.Sprintf("%s code=%d duration_ms=%d", status, code, dur.Milliseconds())
}

// parseFlags parses and validates CLI flags without allowing the flag package to os.Exit.
// This guarantees contract-stable exit codes and ensures we always print the final summary line.
func parseFlags(args []string) (*Config, error) {
	cfg := &Config{}

	fs := flag.NewFlagSet("chartly-tool-profiler", flag.ContinueOnError)
	// Prevent the flag package from printing usage/errors to stderr automatically.
	// We handle all messaging deterministically.
	fs.SetOutput(io.Discard)

	var ws, we string
	var include string

	fs.StringVar(&cfg.Env, "env", "", "Environment (dev|staging|prod)")
	fs.StringVar(&cfg.TargetType, "target-type", "", "Target type (service|workflow|run|connector)")
	fs.StringVar(&cfg.TargetID, "target-id", "", "Target identifier")
	fs.StringVar(&ws, "window-start", "", "Window start (RFC3339)")
	fs.StringVar(&we, "window-end", "", "Window end (RFC3339)")
	fs.StringVar(&cfg.Format, "format", "json", "Output format (json|text)")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "Dry-run (read-only tool; echoed for transparency)")
	fs.StringVar(&cfg.CompareTo, "compare-to", "", "Baseline id for regression comparison (roadmap)")
	fs.StringVar(&cfg.Precision, "precision", "ms", "Numeric precision (ms|s)")
	fs.StringVar(&include, "include-metrics", "", "Comma-separated metric allowlist (roadmap)")

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("flag parse error: %w", err)
	}

	// Required flags.
	if cfg.Env == "" || cfg.TargetType == "" || cfg.TargetID == "" || ws == "" || we == "" {
		return nil, errors.New("missing required flags: --env --target-type --target-id --window-start --window-end")
	}

	// Parse time window.
	var err error
	cfg.WindowStart, err = time.Parse(time.RFC3339, ws)
	if err != nil {
		return nil, fmt.Errorf("invalid window-start: %w", err)
	}
	cfg.WindowEnd, err = time.Parse(time.RFC3339, we)
	if err != nil {
		return nil, fmt.Errorf("invalid window-end: %w", err)
	}
	if !cfg.WindowEnd.After(cfg.WindowStart) {
		return nil, errors.New("window-end must be after window-start")
	}

	// Validate format (truthful support only).
	cfg.Format = strings.ToLower(strings.TrimSpace(cfg.Format))
	if cfg.Format != "json" && cfg.Format != "text" {
		return nil, errors.New("invalid format: supported values are json|text")
	}

	// Validate precision (even if not fully used yet).
	cfg.Precision = strings.ToLower(strings.TrimSpace(cfg.Precision))
	if cfg.Precision != "ms" && cfg.Precision != "s" {
		return nil, errors.New("invalid precision: supported values are ms|s")
	}

	// Parse include-metrics allowlist deterministically.
	cfg.IncludeMetrics = nil
	if strings.TrimSpace(include) != "" {
		parts := strings.Split(include, ",")
		for _, p := range parts {
			v := strings.TrimSpace(p)
			if v == "" {
				continue
			}
			cfg.IncludeMetrics = append(cfg.IncludeMetrics, v)
		}
		sort.Strings(cfg.IncludeMetrics)
	}

	return cfg, nil
}

// maybeAccessDenied is a deterministic, test-friendly access gate.
//
// TEST HOOK CONTRACT:
// - Only honored in non-prod environments (env != "prod").
// - Enabled by setting CHARTLY_PROFILER_DENY=1.
// - Exists solely to allow conformance tests to validate ExitAccessDenied paths without secrets.
//
// This hook MUST NOT affect production behavior.
func maybeAccessDenied(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Env), "prod") {
		return false
	}
	return strings.TrimSpace(os.Getenv("CHARTLY_PROFILER_DENY")) == "1"
}

// runProfiler executes a deterministic, read-only profiling pass.
// This placeholder returns stable data suitable for conformance tests.
func runProfiler(cfg *Config) (*Output, error) {
	// Deterministic placeholder contributors (no randomness).
	breakdown := []BreakdownEntry{
		{Component: "analytics_query", DurationMs: 5000},
		{Component: "connector_sync", DurationMs: 2300},
		{Component: "normalization", DurationMs: 1000},
	}

	// Stable ordering (descending duration, then lexicographic component).
	sort.Slice(breakdown, func(i, j int) bool {
		if breakdown[i].DurationMs == breakdown[j].DurationMs {
			return breakdown[i].Component < breakdown[j].Component
		}
		return breakdown[i].DurationMs > breakdown[j].DurationMs
	})

	// Compute totals deterministically.
	var total int64
	for _, b := range breakdown {
		total += b.DurationMs
	}
	if total <= 0 {
		return nil, errors.New("invalid breakdown total")
	}

	// Compute percent basis points deterministically.
	// Use integer math to avoid float serialization instability.
	var used int64
	for i := range breakdown {
		// floor((duration * 10000) / total)
		bp := (breakdown[i].DurationMs * 10000) / total
		breakdown[i].PctBasisPts = bp
		used += bp
	}

	// Distribute remainder basis points starting from largest contributors
	// to ensure sum is exactly 10000 (100.00%).
	remainder := int64(10000) - used
	for i := int64(0); i < remainder; i++ {
		idx := int(i) % len(breakdown)
		breakdown[idx].PctBasisPts++
	}

	regression := "none"
	if strings.TrimSpace(cfg.CompareTo) != "" {
		// Comparison is roadmap; we report transparently without pretending computation exists.
		regression = "roadmap"
	}

	out := &Output{
		Header: Header{
			ToolVersion:   ToolVersion,
			Environment:   cfg.Env,
			TargetType:    cfg.TargetType,
			TargetID:      cfg.TargetID,
			WindowStart:   cfg.WindowStart.UTC().Format(time.RFC3339),
			WindowEnd:     cfg.WindowEnd.UTC().Format(time.RFC3339),
			DryRun:        cfg.DryRun,
			CompareTo:     strings.TrimSpace(cfg.CompareTo),
			Precision:     cfg.Precision,
			IncludeMetric: cfg.IncludeMetrics,
		},
		Summary: Summary{
			TotalDurationMs: total,
			Bottleneck:      breakdown[0].Component,
			Regression:      regression,
		},
		Breakdown: breakdown,
	}

	return out, nil
}

// printOutput prints output in the requested format.
// JSON is the canonical stable format. Text is human-oriented.
func printOutput(cfg *Config, out *Output) error {
	switch cfg.Format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	case "text":
		fmt.Printf("Target: %s:%s\n", out.Header.TargetType, out.Header.TargetID)
		fmt.Printf("Env: %s\n", out.Header.Environment)
		fmt.Printf("Window: %s -> %s\n", out.Header.WindowStart, out.Header.WindowEnd)
		fmt.Printf("DryRun: %v\n", out.Header.DryRun)
		if out.Header.CompareTo != "" {
			fmt.Printf("CompareTo: %s (roadmap)\n", out.Header.CompareTo)
		}
		fmt.Printf("Total: %dms\n", out.Summary.TotalDurationMs)
		fmt.Printf("Bottleneck: %s\n", out.Summary.Bottleneck)
		fmt.Printf("Regression: %s\n", out.Summary.Regression)
		for _, b := range out.Breakdown {
			// Print percent to one decimal deterministically derived from basis points.
			pct := float64(b.PctBasisPts) / 100.0
			fmt.Printf("- %s: %dms (%.1f%%)\n", b.Component, b.DurationMs, pct)
		}
		return nil
	default:
		return errors.New("format unsupported")
	}
}

func main() {
	start := time.Now()

	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		dur := time.Since(start)
		fmt.Fprintf(os.Stderr, "FAILED code=%d msg=%s\n", ExitInvalidArgs, err.Error())
		fmt.Fprintln(os.Stderr, summaryLine("FAILED", ExitInvalidArgs, dur))
		os.Exit(ExitInvalidArgs)
	}

	if maybeAccessDenied(cfg) {
		dur := time.Since(start)
		fmt.Fprintln(os.Stderr, "FAILED code=5 msg=access denied")
		fmt.Fprintln(os.Stderr, summaryLine("FAILED", ExitAccessDenied, dur))
		os.Exit(ExitAccessDenied)
	}

	out, err := runProfiler(cfg)
	if err != nil {
		dur := time.Since(start)
		fmt.Fprintf(os.Stderr, "FAILED code=%d msg=%s\n", ExitGeneralError, err.Error())
		fmt.Fprintln(os.Stderr, summaryLine("FAILED", ExitGeneralError, dur))
		os.Exit(ExitGeneralError)
	}

	if err := printOutput(cfg, out); err != nil {
		// Output failures are treated as validation failures (schema/format contract).
		dur := time.Since(start)
		fmt.Fprintf(os.Stderr, "FAILED code=%d msg=%s\n", ExitValidationFail, err.Error())
		fmt.Fprintln(os.Stderr, summaryLine("FAILED", ExitValidationFail, dur))
		os.Exit(ExitValidationFail)
	}

	dur := time.Since(start)
	fmt.Fprintln(os.Stderr, summaryLine("OK", ExitSuccess, dur))
	os.Exit(ExitSuccess)
}
