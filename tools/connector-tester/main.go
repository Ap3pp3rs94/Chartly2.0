package main

import (
	"crypto/sha256"
	"encoding/hex"
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

const toolVersion = "0.1.0"

// Exit codes (tools/README.md contract)
const (
	exitSuccess          = 0
	exitGeneralError     = 1
	exitInvalidArgs      = 2
	exitPreconditionFail = 3
	exitValidationFail   = 4
	exitUnsafeBlocked    = 5
)

// Sentinel errors for contract-stable exit code mapping.
var (
	errUnsafeBlocked = errors.New("unsafe_blocked")
	errInvalidArgs   = errors.New("invalid_args")
)

type config struct {
	Mode             string
	Env              string
	Tenant           string
	Project          string
	ConnectorProfile string
	WindowStart      time.Time
	WindowEnd        time.Time
	Format           string
	DryRun           bool
	Apply            bool

	MaxPages   int
	MaxRecords int
	MaxBytes   int

	GoldenPath string
}

type header struct {
	ToolVersion         string   `json:"tool_version"`
	Mode                string   `json:"mode"`
	Execution           string   `json:"execution"` // offline | planned_online
	Environment         string   `json:"env"`
	Tenant              string   `json:"tenant"`
	Project             string   `json:"project"`
	ConnectorProfile    string   `json:"connector_profile"`
	ResolvedProfileHash string   `json:"resolved_profile_hash"`
	WindowStart         string   `json:"window_start"`
	WindowEnd           string   `json:"window_end"`
	DryRun              bool     `json:"dry_run"`
	Apply               bool     `json:"apply"`
	ChecksEnforced      []string `json:"checks_enforced"`
	ChecksPlanned       []string `json:"checks_planned"`
}

type paramKV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type planStep struct {
	Index  int       `json:"index"`
	Method string    `json:"method"`
	Path   string    `json:"path"`
	Params []paramKV `json:"params"`
}

type limits struct {
	MaxPages   int `json:"max_pages"`
	MaxRecords int `json:"max_records"`
	MaxBytes   int `json:"max_bytes"`
}

type plan struct {
	PaginationMode string     `json:"pagination_mode"`
	Limits         limits     `json:"limits"`
	Steps          []planStep `json:"steps"`
	PlanHash       string     `json:"plan_hash"`
}

type finding struct {
	RuleID    string `json:"rule_id"`
	Severity  string `json:"severity"`
	Component string `json:"component"`
	Message   string `json:"message"`
	Metric    string `json:"metric,omitempty"`
	Value     string `json:"value,omitempty"`
}

type validation struct {
	Ok       bool      `json:"ok"`
	Code     string    `json:"code"` // ok | validation_failed | precondition_failed
	Findings []finding `json:"findings"`
}

type output struct {
	Header     header     `json:"header"`
	Plan       plan       `json:"plan"`
	Validation validation `json:"validation"`
}

func summaryLine(status string, code int, dur time.Duration) string {
	return fmt.Sprintf("%s code=%d duration_ms=%d", status, code, dur.Milliseconds())
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func resolvedProfileHash(profileRef string) string {
	// Deterministic stand-in until real resolver exists:
	// hash the profile ref string (no I/O, no secrets).
	return sha256Hex([]byte(strings.TrimSpace(profileRef)))
}

// parseArgs enforces README CLI shape: first positional arg is mode.
// Example: connector-tester plan --env dev ...
func parseArgs(args []string) (*config, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("%w: missing mode argument (plan|run|validate)", errInvalidArgs)
	}

	mode := strings.ToLower(strings.TrimSpace(args[0]))
	if mode != "plan" && mode != "run" && mode != "validate" {
		return nil, fmt.Errorf("%w: invalid mode %q (must be plan|run|validate)", errInvalidArgs, mode)
	}

	cfg := &config{
		Mode:       mode,
		Format:     "json",
		MaxPages:   5,
		MaxRecords: 1000,
		MaxBytes:   1048576,
	}

	fs := flag.NewFlagSet("chartly-tool-connector-tester", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var ws, we string
	fs.StringVar(&cfg.Env, "env", "", "Environment: dev|staging|prod (required)")
	fs.StringVar(&cfg.Tenant, "tenant", "", "Tenant (required)")
	fs.StringVar(&cfg.Project, "project", "", "Project (required)")
	fs.StringVar(&cfg.ConnectorProfile, "connector-profile", "", "Connector profile ref (required)")
	fs.StringVar(&ws, "window-start", "", "RFC3339 window start (required)")
	fs.StringVar(&we, "window-end", "", "RFC3339 window end (required)")
	fs.StringVar(&cfg.Format, "format", "json", "Output format: json|text")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "Dry-run (messaging only; never enables network)")
	fs.BoolVar(&cfg.Apply, "apply", false, "Apply (required for run)")
	fs.IntVar(&cfg.MaxPages, "max-pages", cfg.MaxPages, "Max pages cap")
	fs.IntVar(&cfg.MaxRecords, "max-records", cfg.MaxRecords, "Max records cap")
	fs.IntVar(&cfg.MaxBytes, "max-bytes", cfg.MaxBytes, "Max bytes cap")
	fs.StringVar(&cfg.GoldenPath, "golden", "", "Golden artifact path (validate mode)")

	if err := fs.Parse(args[1:]); err != nil {
		return nil, fmt.Errorf("%w: flag parse error: %s", errInvalidArgs, err.Error())
	}

	cfg.Env = strings.ToLower(strings.TrimSpace(cfg.Env))
	if cfg.Env != "dev" && cfg.Env != "staging" && cfg.Env != "prod" {
		return nil, fmt.Errorf("%w: invalid --env (must be dev|staging|prod)", errInvalidArgs)
	}
	if strings.TrimSpace(cfg.Tenant) == "" || strings.TrimSpace(cfg.Project) == "" {
		return nil, fmt.Errorf("%w: missing required flags --tenant --project", errInvalidArgs)
	}
	if strings.TrimSpace(cfg.ConnectorProfile) == "" {
		return nil, fmt.Errorf("%w: missing required flag --connector-profile", errInvalidArgs)
	}
	if ws == "" || we == "" {
		return nil, fmt.Errorf("%w: missing required flags --window-start --window-end", errInvalidArgs)
	}

	var err error
	cfg.WindowStart, err = time.Parse(time.RFC3339, ws)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid --window-start", errInvalidArgs)
	}
	cfg.WindowEnd, err = time.Parse(time.RFC3339, we)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid --window-end", errInvalidArgs)
	}
	if !cfg.WindowEnd.After(cfg.WindowStart) {
		return nil, fmt.Errorf("%w: window-end must be after window-start", errInvalidArgs)
	}

	cfg.Format = strings.ToLower(strings.TrimSpace(cfg.Format))
	if cfg.Format != "json" && cfg.Format != "text" {
		return nil, fmt.Errorf("%w: invalid --format (must be json|text)", errInvalidArgs)
	}

	if cfg.MaxPages <= 0 || cfg.MaxRecords <= 0 || cfg.MaxBytes <= 0 {
		return nil, fmt.Errorf("%w: caps must be > 0", errInvalidArgs)
	}

	// Mode gating rules (contract):
	if cfg.Mode == "run" && !cfg.Apply {
		return nil, fmt.Errorf("%w: run requires --apply", errUnsafeBlocked)
	}
	if cfg.Mode == "run" && cfg.Env == "prod" {
		return nil, fmt.Errorf("%w: run in prod is forbidden by default", errUnsafeBlocked)
	}

	return cfg, nil
}

func buildPlan(cfg *config) plan {
	// Deterministic placeholder plan: choose pagination mode based on profile ref content.
	mode := "cursor"
	lref := strings.ToLower(cfg.ConnectorProfile)
	if strings.Contains(lref, "offset") {
		mode = "offset"
	} else if strings.Contains(lref, "page") {
		mode = "page"
	} else if strings.Contains(lref, "time-window") {
		mode = "time-window"
	}

	params := []paramKV{
		{Key: "page_token", Value: "<initial>"},
		{Key: "window_end", Value: cfg.WindowEnd.UTC().Format(time.RFC3339)},
		{Key: "window_start", Value: cfg.WindowStart.UTC().Format(time.RFC3339)},
	}
	sort.Slice(params, func(i, j int) bool { return params[i].Key < params[j].Key })

	p := plan{
		PaginationMode: mode,
		Limits: limits{
			MaxPages:   cfg.MaxPages,
			MaxRecords: cfg.MaxRecords,
			MaxBytes:   cfg.MaxBytes,
		},
		Steps: []planStep{
			{Index: 1, Method: "GET", Path: "/connector/sync", Params: params},
		},
		PlanHash: "",
	}

	// Deterministic plan hash:
	// - no maps in hashed content
	// - stable struct field order
	// - stable slice order (params already sorted)
	raw, _ := json.Marshal(p)
	p.PlanHash = sha256Hex(raw)
	return p
}

func validateContract(cfg *config, p plan) validation {
	findings := make([]finding, 0, 8)

	// Caps must exist and be positive.
	if p.Limits.MaxPages <= 0 || p.Limits.MaxRecords <= 0 || p.Limits.MaxBytes <= 0 {
		findings = append(findings, finding{
			RuleID:    "connector_tester.limits.invalid",
			Severity:  "error",
			Component: "plan",
			Message:   "limits must be positive",
		})
	}

	// SSRF/egress guardrails check (placeholder-level): reject obvious blocked destination markers in profile ref.
	lref := strings.ToLower(cfg.ConnectorProfile)
	blockedMarkers := []string{"localhost", "127.0.0.1", "169.254.", "metadata"}
	for _, m := range blockedMarkers {
		if strings.Contains(lref, m) {
			findings = append(findings, finding{
				RuleID:    "security.ssrf.blocked",
				Severity:  "error",
				Component: "security",
				Message:   "connector profile ref indicates a blocked destination marker",
				Metric:    "connector_profile",
				Value:     m,
			})
		}
	}

	// Stable ordering.
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Severity != findings[j].Severity {
			return findings[i].Severity > findings[j].Severity
		}
		if findings[i].RuleID != findings[j].RuleID {
			return findings[i].RuleID < findings[j].RuleID
		}
		if findings[i].Component != findings[j].Component {
			return findings[i].Component < findings[j].Component
		}
		return findings[i].Message < findings[j].Message
	})

	if len(findings) == 0 {
		return validation{Ok: true, Code: "ok", Findings: nil}
	}
	return validation{Ok: false, Code: "validation_failed", Findings: findings}
}

func printJSON(out output) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func printText(out output) error {
	fmt.Printf("Mode: %s\n", out.Header.Mode)
	fmt.Printf("Execution: %s\n", out.Header.Execution)
	fmt.Printf("Env: %s\n", out.Header.Environment)
	fmt.Printf("Tenant/Project: %s/%s\n", out.Header.Tenant, out.Header.Project)
	fmt.Printf("Profile: %s\n", out.Header.ConnectorProfile)
	fmt.Printf("ProfileHash: %s\n", out.Header.ResolvedProfileHash)
	fmt.Printf("Window: %s -> %s\n", out.Header.WindowStart, out.Header.WindowEnd)
	fmt.Printf("DryRun: %v Apply: %v\n", out.Header.DryRun, out.Header.Apply)
	fmt.Printf("Plan: mode=%s hash=%s steps=%d\n", out.Plan.PaginationMode, out.Plan.PlanHash, len(out.Plan.Steps))
	fmt.Printf("Validation: ok=%v code=%s findings=%d\n", out.Validation.Ok, out.Validation.Code, len(out.Validation.Findings))
	if len(out.Header.ChecksEnforced) > 0 {
		fmt.Printf("ChecksEnforced: %s\n", strings.Join(out.Header.ChecksEnforced, ","))
	}
	if len(out.Header.ChecksPlanned) > 0 {
		fmt.Printf("ChecksPlanned: %s\n", strings.Join(out.Header.ChecksPlanned, ","))
	}
	return nil
}

func canonicalJSON(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func computePlanHash(p plan) string {
	tmp := p
	tmp.PlanHash = ""
	raw, _ := json.Marshal(tmp)
	return sha256Hex(raw)
}

func readGoldenPlan(path string) (plan, error) {
	var p plan
	b, err := os.ReadFile(path)
	if err != nil {
		return p, err
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, err
	}
	return p, nil
}

func comparePlanToGolden(cur plan, golden plan) validation {
	// Validate golden integrity first.
	goldenHash := computePlanHash(golden)
	if strings.TrimSpace(golden.PlanHash) == "" || goldenHash != golden.PlanHash {
		return validation{
			Ok:   false,
			Code: "validation_failed",
			Findings: []finding{{
				RuleID:    "validate.golden.plan_hash_invalid",
				Severity:  "error",
				Component: "validate",
				Message:   "golden plan hash is missing or invalid",
				Metric:    "plan_hash",
				Value:     golden.PlanHash,
			}},
		}
	}

	// Compare normalized plans (PlanHash cleared).
	cur.PlanHash = ""
	golden.PlanHash = ""
	curBytes, _ := canonicalJSON(cur)
	goldenBytes, _ := canonicalJSON(golden)
	if string(curBytes) != string(goldenBytes) {
		return validation{
			Ok:   false,
			Code: "validation_failed",
			Findings: []finding{{
				RuleID:    "validate.plan_mismatch",
				Severity:  "error",
				Component: "validate",
				Message:   "computed plan does not match golden plan",
			}},
		}
	}
	return validation{Ok: true, Code: "ok", Findings: nil}
}

func main() {
	start := time.Now()

	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		code := exitInvalidArgs
		if errors.Is(err, errUnsafeBlocked) {
			code = exitUnsafeBlocked
		}
		dur := time.Since(start)
		fmt.Fprintf(os.Stderr, "FAILED code=%d msg=%s\n", code, err.Error())
		fmt.Fprintln(os.Stderr, summaryLine("FAILED", code, dur))
		os.Exit(code)
	}

	p := buildPlan(cfg)
	v := validateContract(cfg, p)

	// Deterministic, explicit check disclosures (prevents doc/tool drift).
	checksEnforced := []string{"limits.positive", "security.ssrf_marker_block"}
	checksPlanned := []string{"pagination.correctness", "retry.classification", "rate_limit.enforcement", "tls.verification", "egress.allowlist"}
	sort.Strings(checksEnforced)
	sort.Strings(checksPlanned)

	out := output{
		Header: header{
			ToolVersion:         toolVersion,
			Mode:                cfg.Mode,
			Execution:           "offline",
			Environment:         cfg.Env,
			Tenant:              cfg.Tenant,
			Project:             cfg.Project,
			ConnectorProfile:    cfg.ConnectorProfile,
			ResolvedProfileHash: resolvedProfileHash(cfg.ConnectorProfile),
			WindowStart:         cfg.WindowStart.UTC().Format(time.RFC3339),
			WindowEnd:           cfg.WindowEnd.UTC().Format(time.RFC3339),
			DryRun:              cfg.DryRun,
			Apply:               cfg.Apply,
			ChecksEnforced:      checksEnforced,
			ChecksPlanned:       checksPlanned,
		},
		Plan:       p,
		Validation: v,
	}

	switch cfg.Mode {
	case "plan":
		// offline only
	case "validate":
		// Offline. If golden provided, compare deterministically.
		if strings.TrimSpace(cfg.GoldenPath) != "" {
			gp := strings.TrimSpace(cfg.GoldenPath)
			goldenPlan, gerr := readGoldenPlan(gp)
			if gerr != nil {
				out.Validation = validation{
					Ok:   false,
					Code: "precondition_failed",
					Findings: []finding{{
						RuleID:    "validate.golden.read_failed",
						Severity:  "error",
						Component: "validate",
						Message:   "unable to read golden plan",
						Metric:    "golden",
						Value:     gp,
					}},
				}
			} else {
				out.Validation = comparePlanToGolden(p, goldenPlan)
			}
		}
	case "run":
		// Placeholder: run is intended to be online+gated, but network execution is not available.
		out.Header.Execution = "unavailable_online"
		out.Validation = validation{
			Ok:   false,
			Code: "precondition_failed",
			Findings: []finding{{
				RuleID:    "run.unavailable",
				Severity:  "error",
				Component: "run",
				Message:   "network execution is unavailable; refusing to simulate online behavior",
			}},
		}
	}

	var perr error
	if cfg.Format == "json" {
		perr = printJSON(out)
	} else {
		perr = printText(out)
	}

	if perr != nil {
		dur := time.Since(start)
		fmt.Fprintf(os.Stderr, "FAILED code=%d msg=output_error\n", exitValidationFail)
		fmt.Fprintln(os.Stderr, summaryLine("FAILED", exitValidationFail, dur))
		os.Exit(exitValidationFail)
	}

	switch out.Validation.Code {
	case "validation_failed":
		dur := time.Since(start)
		fmt.Fprintln(os.Stderr, summaryLine("FAILED", exitValidationFail, dur))
		os.Exit(exitValidationFail)
	case "precondition_failed":
		dur := time.Since(start)
		fmt.Fprintln(os.Stderr, summaryLine("FAILED", exitPreconditionFail, dur))
		os.Exit(exitPreconditionFail)
	default:
		dur := time.Since(start)
		fmt.Fprintln(os.Stderr, summaryLine("OK", exitSuccess, dur))
		os.Exit(exitSuccess)
	}
}
