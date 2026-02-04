package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// Sentinel errors for stable exit mapping.
var (
	errUnsafeBlocked = errors.New("unsafe_blocked")
	errInvalidArgs   = errors.New("invalid_args")
	errPrecondition  = errors.New("precondition_failed")
)

type config struct {
	Mode         string
	Env          string
	MigrationID  string
	TargetVer    string
	Tenant       string
	Project      string
	Format       string
	Apply        bool
	DryRun       bool
	ProdOverride string
	PlanPath     string
	OutDir       string
}

type header struct {
	ToolVersion  string `json:"tool_version"`
	Mode         string `json:"mode"`
	Execution    string `json:"execution"` // offline | apply_local_stub
	Env          string `json:"env"`
	MigrationID  string `json:"migration_id"`
	TargetVer    string `json:"target_version"`
	Tenant       string `json:"tenant,omitempty"`
	Project      string `json:"project,omitempty"`
	Apply        bool   `json:"apply"`
	DryRun       bool   `json:"dry_run"`
	ProdOverride string `json:"prod_override,omitempty"`
}

type step struct {
	Index          int      `json:"index"`
	StepID         string   `json:"step_id"`
	Type           string   `json:"type"`
	Description    string   `json:"description"`
	IdempotencyKey string   `json:"idempotency_key"`
	Preconditions  []string `json:"preconditions"`
	Actions        []string `json:"actions"`
	Postconditions []string `json:"postconditions"`
}

type rollback struct {
	Index       int      `json:"index"`
	StepID      string   `json:"step_id"`
	Description string   `json:"description"`
	Actions     []string `json:"actions"`
}

type plan struct {
	Header   header     `json:"header"`
	Steps    []step     `json:"steps"`
	PlanHash string     `json:"plan_hash"`
	Rollback []rollback `json:"rollback"`
}

type outcome struct {
	Index   int    `json:"index"`
	StepID  string `json:"step_id"`
	Status  string `json:"status"`  // applied | skipped
	Message string `json:"message"` // deterministic
}

type finding struct {
	RuleID    string `json:"rule_id"`
	Severity  string `json:"severity"` // info|warn|error
	Component string `json:"component"`
	Message   string `json:"message"`
	Metric    string `json:"metric,omitempty"`
	Value     string `json:"value,omitempty"`
}

type applyReport struct {
	Header   header     `json:"header"`
	PlanHash string     `json:"plan_hash"`
	Outcomes []outcome  `json:"outcomes"`
	Ok       bool       `json:"ok"`
	Code     string     `json:"code"` // ok | precondition_failed | validation_failed
	Findings []finding  `json:"findings,omitempty"`
	Rollback []rollback `json:"rollback"`
}

func summaryLine(status string, code int, dur time.Duration) string {
	return fmt.Sprintf("%s code=%d duration_ms=%d", status, code, dur.Milliseconds())
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// canonicalJSONBytes produces deterministic JSON bytes for hashing and output.
// Contract note: plans and reports MUST NOT contain maps. Only structs + slices.
// Slices are sorted explicitly before hashing.
func canonicalJSONBytes(v any) []byte {
	b, _ := json.MarshalIndent(v, "", "  ")
	return append(b, '\n')
}

// computePlanHash computes sha256 over canonical JSON bytes of the plan with PlanHash cleared.
// This enforces "hash matches bytes" deterministically.
func computePlanHash(p plan) string {
	tmp := p
	tmp.PlanHash = ""
	raw := canonicalJSONBytes(tmp)
	return sha256Hex(raw)
}

func parseArgs(args []string) (*config, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("%w: missing mode argument (plan|apply|verify)", errInvalidArgs)

	}
	mode := strings.ToLower(strings.TrimSpace(args[0]))
	if mode != "plan" && mode != "apply" && mode != "verify" {
		return nil, fmt.Errorf("%w: invalid mode %q (must be plan|apply|verify)", errInvalidArgs, mode)

	}
	cfg := &config{
		Mode:   mode,
		Format: "json",
		OutDir: "reports",
	}

	fs := flag.NewFlagSet("chartly-tool-migration-tool", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	fs.StringVar(&cfg.Env, "env", "", "Environment: dev|staging|prod (required)")
	fs.StringVar(&cfg.MigrationID, "migration", "", "Migration id (required)")
	fs.StringVar(&cfg.TargetVer, "target-version", "", "Target version semver (required)")
	fs.StringVar(&cfg.Tenant, "tenant", "", "Tenant (optional)")
	fs.StringVar(&cfg.Project, "project", "", "Project (optional)")
	fs.StringVar(&cfg.Format, "format", "json", "Output format: json|text")
	fs.BoolVar(&cfg.Apply, "apply", false, "Apply (required for apply mode)")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "Dry-run (apply mode only; no writes)")
	fs.StringVar(&cfg.ProdOverride, "prod-override", "", "Ticket id required for prod apply")
	fs.StringVar(&cfg.PlanPath, "plan", "", "Explicit plan file path (verify mode)")
	fs.StringVar(&cfg.OutDir, "out", cfg.OutDir, "Output directory for reports (apply mode)")

	if err := fs.Parse(args[1:]); err != nil {
		return nil, fmt.Errorf("%w: flag parse error: %s", errInvalidArgs, err.Error())

	}
	cfg.Env = strings.ToLower(strings.TrimSpace(cfg.Env))
	if cfg.Env != "dev" && cfg.Env != "staging" && cfg.Env != "prod" {
		return nil, fmt.Errorf("%w: invalid --env (must be dev|staging|prod)", errInvalidArgs)

	}
	if strings.TrimSpace(cfg.MigrationID) == "" || strings.TrimSpace(cfg.TargetVer) == "" {
		return nil, fmt.Errorf("%w: missing required flags --migration --target-version", errInvalidArgs)

	}
	cfg.Format = strings.ToLower(strings.TrimSpace(cfg.Format))
	if cfg.Format != "json" && cfg.Format != "text" {
		return nil, fmt.Errorf("%w: invalid --format (must be json|text)", errInvalidArgs)

	}

	// Safety gates:
	if cfg.Mode == "apply" && !cfg.Apply {
		return nil, fmt.Errorf("%w: apply mode requires --apply", errUnsafeBlocked)

	}
	if cfg.Mode == "apply" && cfg.Env == "prod" && strings.TrimSpace(cfg.ProdOverride) == "" {
		return nil, fmt.Errorf("%w: prod apply requires --prod-override <ticket-id>", errUnsafeBlocked)

	}
	return cfg, nil
}

func buildPlan(cfg *config) plan {
	h := header{
		ToolVersion:  toolVersion,
		Mode:         cfg.Mode,
		Execution:    "offline",
		Env:          cfg.Env,
		MigrationID:  cfg.MigrationID,
		TargetVer:    cfg.TargetVer,
		Tenant:       strings.TrimSpace(cfg.Tenant),
		Project:      strings.TrimSpace(cfg.Project),
		Apply:        cfg.Apply,
		DryRun:       cfg.DryRun,
		ProdOverride: strings.TrimSpace(cfg.ProdOverride),
	}

	typ := "config"
	lid := strings.ToLower(cfg.MigrationID)
	switch {
	case strings.Contains(lid, "profile"):
		typ = "profile"
	case strings.Contains(lid, "schema"):
		typ = "schema"
	case strings.Contains(lid, "data"):
		typ = "data"
	case strings.Contains(lid, "index"):
		typ = "index"
	}

	scopeKey := strings.Join([]string{
		cfg.Env,
		strings.TrimSpace(cfg.Tenant),
		strings.TrimSpace(cfg.Project),
		cfg.MigrationID,
		cfg.TargetVer,
	}, "|")

	steps := []step{
		{Index: 1, StepID: "step.prepare", Type: "config", Description: "Prepare: validate preconditions and freeze inputs",
			IdempotencyKey: sha256Hex([]byte(scopeKey + "|step.prepare")),
			Preconditions:  sortedStrings([]string{"inputs_present", "target_version_valid"}),
			Actions:        sortedStrings([]string{"compute_plan_hash", "record_scope"}),
			Postconditions: sortedStrings([]string{"plan_frozen"}),
		},
		{Index: 2, StepID: "step.migrate", Type: typ, Description: "Apply migration changes (idempotent, deterministic)",
			IdempotencyKey: sha256Hex([]byte(scopeKey + "|step.migrate")),
			Preconditions:  sortedStrings([]string{"plan_frozen"}),
			Actions:        sortedStrings([]string{"perform_idempotent_change", "verify_postconditions"}),
			Postconditions: sortedStrings([]string{"target_version_reached"}),
		},
		{Index: 3, StepID: "step.cleanup", Type: "cleanup", Description: "Cleanup: remove deprecated artifacts (safe, optional)",
			IdempotencyKey: sha256Hex([]byte(scopeKey + "|step.cleanup")),
			Preconditions:  sortedStrings([]string{"target_version_reached"}),
			Actions:        sortedStrings([]string{"no_destructive_cleanup_by_default"}),
			Postconditions: sortedStrings([]string{"system_stable"}),
		},
	}

	rb := []rollback{
		{Index: 1, StepID: "rollback.cleanup", Description: "Rollback cleanup changes", Actions: sortedStrings([]string{"reverse_cleanup_if_any"})},
		{Index: 2, StepID: "rollback.migrate", Description: "Rollback migration changes to previous version", Actions: sortedStrings([]string{"redeploy_previous_version_refs"})},
		{Index: 3, StepID: "rollback.prepare", Description: "Rollback preparation markers (no-op)", Actions: sortedStrings([]string{"noop"})},
	}

	p := plan{
		Header:   h,
		Steps:    steps,
		PlanHash: "",
		Rollback: rb,
	}
	p.PlanHash = computePlanHash(p)
	return p
}

func sortedStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		t := strings.TrimSpace(s)
		if t == "" {
			continue

		}
		out = append(out, t)

	}
	sort.Strings(out)
	return out
}

// readPlanFileStrict enforces plan contract integrity:
// - strict decoding (no unknown fields)
// - deterministic error classification
func readPlanFileStrict(path string) (plan, error) {
	var p plan
	b, err := os.ReadFile(path)
	if err != nil {
		return p, fmt.Errorf("%w: missing plan file", errPrecondition)

	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, fmt.Errorf("%w: invalid plan json", errInvalidArgs)

	}
	return p, nil
}

func writeJSONFile(dir string, name string, b []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err

	}
	fp := filepath.Join(dir, name)
	return os.WriteFile(fp, b, 0o644)
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func printTextPlan(p plan) {
	fmt.Printf("migration-tool %s\n", p.Header.Mode)
	fmt.Printf("env=%s migration=%s target=%s\n", p.Header.Env, p.Header.MigrationID, p.Header.TargetVer)
	if p.Header.Tenant != "" || p.Header.Project != "" {
		fmt.Printf("scope=%s/%s\n", p.Header.Tenant, p.Header.Project)

	}
	fmt.Printf("plan_hash=%s steps=%d rollback=%d\n", p.PlanHash, len(p.Steps), len(p.Rollback))
}

func printTextReport(r applyReport) {
	fmt.Printf("migration-tool %s\n", r.Header.Mode)
	fmt.Printf("env=%s migration=%s target=%s\n", r.Header.Env, r.Header.MigrationID, r.Header.TargetVer)
	fmt.Printf("plan_hash=%s ok=%v code=%s outcomes=%d\n", r.PlanHash, r.Ok, r.Code, len(r.Outcomes))
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
	genPlan := buildPlan(cfg)

	switch cfg.Mode {
	case "plan":
		if cfg.Format == "json" {
			if err := printJSON(genPlan); err != nil {
				dur := time.Since(start)
				fmt.Fprintln(os.Stderr, summaryLine("FAILED", exitValidationFail, dur))
				os.Exit(exitValidationFail)

			}
		} else {
			printTextPlan(genPlan)

		}
		dur := time.Since(start)
		fmt.Fprintln(os.Stderr, summaryLine("OK", exitSuccess, dur))
		os.Exit(exitSuccess)

	case "verify":
		if strings.TrimSpace(cfg.PlanPath) == "" {
			out := map[string]any{"ok": false, "code": "precondition_failed", "message": "missing_plan_file"}
			if cfg.Format == "json" {
				_ = printJSON(out)
			} else {
				fmt.Println("verify precondition_failed missing_plan_file")

			}
			dur := time.Since(start)
			fmt.Fprintln(os.Stderr, summaryLine("FAILED", exitPreconditionFail, dur))
			os.Exit(exitPreconditionFail)

		}
		filePlan, perr := readPlanFileStrict(cfg.PlanPath)
		if perr != nil {
			code := exitPreconditionFail
			if errors.Is(perr, errInvalidArgs) {
				code = exitInvalidArgs

			}
			out := map[string]any{"ok": false, "code": "precondition_failed", "message": perr.Error()}
			if cfg.Format == "json" {
				_ = printJSON(out)
			} else {
				fmt.Println("verify precondition_failed")

			}
			dur := time.Since(start)
			fmt.Fprintln(os.Stderr, summaryLine("FAILED", code, dur))
			os.Exit(code)

		}
		// Integrity check: plan_hash must match canonical bytes of the plan itself.
		fileComputed := computePlanHash(filePlan)
		if fileComputed != filePlan.PlanHash {
			out := map[string]any{
				"ok":            false,
				"code":          "validation_failed",
				"message":       "plan_hash_integrity_failed",
				"expected_hash": fileComputed,
				"actual_hash":   filePlan.PlanHash,
			}
			if cfg.Format == "json" {
				_ = printJSON(out)
			} else {
				fmt.Println("verify validation_failed plan_hash_integrity_failed")

			}
			dur := time.Since(start)
			fmt.Fprintln(os.Stderr, summaryLine("FAILED", exitValidationFail, dur))
			os.Exit(exitValidationFail)

		}
		// Drift check: file plan hash must equal regenerated plan hash.
		if fileComputed != genPlan.PlanHash {
			out := map[string]any{
				"ok":            false,
				"code":          "validation_failed",
				"message":       "plan_hash_mismatch",
				"expected_hash": genPlan.PlanHash,
				"actual_hash":   fileComputed,
			}
			if cfg.Format == "json" {
				_ = printJSON(out)
			} else {
				fmt.Println("verify validation_failed plan_hash_mismatch")

			}
			dur := time.Since(start)
			fmt.Fprintln(os.Stderr, summaryLine("FAILED", exitValidationFail, dur))
			os.Exit(exitValidationFail)

		}
		out := map[string]any{"ok": true, "code": "ok", "plan_hash": genPlan.PlanHash}
		if cfg.Format == "json" {
			_ = printJSON(out)
		} else {
			fmt.Println("verify ok")

		}
		dur := time.Since(start)
		fmt.Fprintln(os.Stderr, summaryLine("OK", exitSuccess, dur))
		os.Exit(exitSuccess)

	case "apply":
		// Honest stub executor: local-only, no remote side effects.
		h := genPlan.Header
		h.Mode = "apply"
		h.Execution = "apply_local_stub"

		outcomes := make([]outcome, 0, len(genPlan.Steps))
		for _, s := range genPlan.Steps {
			st := "applied"
			if cfg.DryRun {
				st = "skipped"

			}
			outcomes = append(outcomes, outcome{
				Index:   s.Index,
				StepID:  s.StepID,
				Status:  st,
				Message: "local_stub_executor",
			})

		}
		sort.Slice(outcomes, func(i, j int) bool { return outcomes[i].Index < outcomes[j].Index })

		report := applyReport{
			Header:   h,
			PlanHash: genPlan.PlanHash,
			Outcomes: outcomes,
			Ok:       true,
			Code:     "ok",
			Findings: []finding{{
				RuleID:    "apply.stub_executor",
				Severity:  "warn",
				Component: "apply",
				Message:   "apply executed via local stub executor (no remote side effects)",
			}},
			Rollback: genPlan.Rollback,
		}

		if !cfg.DryRun {
			b := canonicalJSONBytes(report)
			name := fmt.Sprintf("migration_%s_%s_report.json", safeFile(cfg.MigrationID), safeFile(cfg.TargetVer))
			if werr := writeJSONFile(cfg.OutDir, name, b); werr != nil {
				out := map[string]any{"ok": false, "code": "precondition_failed", "message": "write_failed"}
				if cfg.Format == "json" {
					_ = printJSON(out)
				} else {
					fmt.Println("apply precondition_failed write_failed")

				}
				dur := time.Since(start)
				fmt.Fprintln(os.Stderr, summaryLine("FAILED", exitPreconditionFail, dur))
				os.Exit(exitPreconditionFail)

			}

		}
		if cfg.Format == "json" {
			if err := printJSON(report); err != nil {
				dur := time.Since(start)
				fmt.Fprintln(os.Stderr, summaryLine("FAILED", exitValidationFail, dur))
				os.Exit(exitValidationFail)

			}
		} else {
			printTextReport(report)

		}

		// Exit based solely on report.Code (no RuleID matching).
		if report.Code == "validation_failed" {
			dur := time.Since(start)
			fmt.Fprintln(os.Stderr, summaryLine("FAILED", exitValidationFail, dur))
			os.Exit(exitValidationFail)

		}
		if report.Code == "precondition_failed" {
			dur := time.Since(start)
			fmt.Fprintln(os.Stderr, summaryLine("FAILED", exitPreconditionFail, dur))
			os.Exit(exitPreconditionFail)

		}
		dur := time.Since(start)
		fmt.Fprintln(os.Stderr, summaryLine("OK", exitSuccess, dur))
		os.Exit(exitSuccess)

	default:
		dur := time.Since(start)
		fmt.Fprintln(os.Stderr, summaryLine("FAILED", exitGeneralError, dur))
		os.Exit(exitGeneralError)

	}
}

func safeFile(s string) string {
	out := strings.TrimSpace(s)
	out = strings.ReplaceAll(out, " ", "_")
	out = strings.ReplaceAll(out, "/", "_")
	out = strings.ReplaceAll(out, "\\", "_")
	out = strings.ReplaceAll(out, ":", "_")
	return out
}
