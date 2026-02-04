package validators

import (
    "math"
    "sort"
    "strings"
)

// NOTE: This file is intentionally self-contained so the validator contract can compile
// even before types.go/registry.go exist. When shared types are introduced, this file
// should be adjusted to reuse them (without changing JSON field meanings).

// Severity expresses finding severity.
type Severity string

const (
    SevInfo  Severity = "info"
    SevWarn  Severity = "warn"
    SevError Severity = "error"
)

// FindingClass determines how a finding affects the validator ResultCode.
type FindingClass string

const (
    ClassPrecondition FindingClass = "precondition"
    ClassValidation   FindingClass = "validation"
)

// Finding is a deterministic, actionable validation finding.
type Finding struct {
    RuleID    string   `json:"rule_id"`
    Severity  Severity `json:"severity"`
    Component string   `json:"component"`
    Message   string   `json:"message"`
    Metric    string   `json:"metric,omitempty"`
    Value     string   `json:"value,omitempty"`

    // Class is intentionally NOT serialized to keep external schema stable while this tool evolves.
    // Classification is handled internally and does not depend on RuleID naming.
    class FindingClass
}

// Input is the immutable snapshot fed to validators.
// Observed values MUST be finite; NaN/Inf are invalid and must produce precondition_failed.
type Input struct {
    Env         string             `json:"env"`
    TargetType  string             `json:"target_type"`
    TargetID    string             `json:"target_id"`
    WindowStart string             `json:"window_start"`
    WindowEnd   string             `json:"window_end"`
    Observed    map[string]float64 `json:"observed"`

    ObservedByComponent map[string]map[string]float64 `json:"observed_by_component,omitempty"`
}

// ResultCode is a stable machine code for validator outcomes.
type ResultCode string

const (
    CodeOK               ResultCode = "ok"
    CodeValidationFailed ResultCode = "validation_failed"
    CodePreconditionFail ResultCode = "precondition_failed"
    CodeNotApplicable    ResultCode = "not_applicable"
)

// Result is the deterministic validator outcome.
type Result struct {
    Ok       bool      `json:"ok"`
    Code     ResultCode `json:"code"`
    Findings []Finding `json:"findings"`
}

// Validator is the standard interface for profiler validators.
type Validator interface {
    ID() string
    Run(in Input) Result
}

// SnapshotValidator enforces baseline input/snapshot hygiene for the profiler.
//
// Taxonomy alignment (README.md):
// - precondition_failed: missing/invalid snapshot data (missing fields, missing metrics, NaN/Inf)
// - validation_failed: true contract violations (unsupported target types, invalid config)
//
// Mixed findings rule (explicit):
// - If any validation finding exists, ResultCode is validation_failed (even if preconditions also exist).
//   This ensures true contract violations are not hidden by missing-data issues.
type SnapshotValidator struct {
    // RequiredMetrics must be present in Input.Observed for the validator to pass.
    // If empty, the validator only enforces structural + numeric hygiene.
    RequiredMetrics []string

    // AppliesToTargetTypes is an allowlist. If empty, applies to all target types.
    AppliesToTargetTypes []string

    // AllowedTargetTypes defines the contractually allowed target types. If empty, no validation is performed.
    // If non-empty and in.TargetType is not in the set, this is a validation_failed.
    AllowedTargetTypes []string
}

// NewSnapshotValidator constructs a deterministic validator configuration.
// Sorting is applied to ensure stable behavior.
func NewSnapshotValidator(requiredMetrics []string, appliesTo []string, allowedTargetTypes []string) *SnapshotValidator {
    rm := dedupeAndSort(requiredMetrics)
    at := dedupeAndSortLower(appliesTo)
    allowed := dedupeAndSortLower(allowedTargetTypes)
    return &SnapshotValidator{
        RequiredMetrics:      rm,
        AppliesToTargetTypes: at,
        AllowedTargetTypes:   allowed,
    }
}

// ID returns the globally unique validator ID.
// Convention: operability.<short_name>
func (v *SnapshotValidator) ID() string { return "operability.snapshot_hygiene" }

// Run executes validation deterministically with stable finding ordering.
func (v *SnapshotValidator) Run(in Input) Result {
    if !v.appliesTo(in.TargetType) {
        return Result{Ok: true, Code: CodeNotApplicable, Findings: nil}
    }

    findings := make([]Finding, 0, 16)
    hasValidation := false

    // (A) Validation failures: true contract violations, not missing data.
    // If target_type is empty, that is handled as a precondition below (not a validation).
    if len(v.AllowedTargetTypes) > 0 {
        tt := strings.ToLower(strings.TrimSpace(in.TargetType))
        if tt != "" && !containsLower(v.AllowedTargetTypes, tt) {
            findings = append(findings, Finding{
                RuleID:    "operability.snapshot_hygiene.target_type_unsupported",
                Severity:  SevError,
                Component: "input",
                Message:   "target_type is not supported by this validator contract",
                Metric:    "target_type",
                Value:     in.TargetType,
                class:     ClassValidation,
            })
            hasValidation = true
        }
    }

    // (B) Precondition failures: missing required fields.
    if strings.TrimSpace(in.Env) == "" {
        findings = append(findings, findingPrecond("operability.snapshot_hygiene.env_required", "env is required", "env", ""))
    }
    if strings.TrimSpace(in.TargetType) == "" {
        findings = append(findings, findingPrecond("operability.snapshot_hygiene.target_type_required", "target_type is required", "target_type", ""))
    }
    if strings.TrimSpace(in.TargetID) == "" {
        findings = append(findings, findingPrecond("operability.snapshot_hygiene.target_id_required", "target_id is required", "target_id", ""))
    }
    if strings.TrimSpace(in.WindowStart) == "" {
        findings = append(findings, findingPrecond("operability.snapshot_hygiene.window_start_required", "window_start is required", "window_start", ""))
    }
    if strings.TrimSpace(in.WindowEnd) == "" {
        findings = append(findings, findingPrecond("operability.snapshot_hygiene.window_end_required", "window_end is required", "window_end", ""))
    }

    // Observed snapshot presence is a hard precondition.
    if in.Observed == nil {
        findings = append(findings, findingPrecond("operability.snapshot_hygiene.observed_required", "observed metrics snapshot is required", "observed", ""))
        sortFindings(findings)
        return finalize(findings, hasValidation)
    }

    // (C) Precondition failures: float hygiene in Observed.
    for k, val := range in.Observed {
        if math.IsNaN(val) || math.IsInf(val, 0) {
            findings = append(findings, Finding{
                RuleID:    "operability.snapshot_hygiene.non_finite_metric",
                Severity:  SevError,
                Component: "snapshot",
                Message:   "observed metric value is non-finite (NaN/Inf); collector may have division-by-zero or invalid input",
                Metric:    k,
                Value:     "non-finite",
                class:     ClassPrecondition,
            })
        }
    }

    // (D) Precondition failures: float hygiene in ObservedByComponent (if present).
    for comp, m := range in.ObservedByComponent {
        for k, val := range m {
            if math.IsNaN(val) || math.IsInf(val, 0) {
                findings = append(findings, Finding{
                    RuleID:    "operability.snapshot_hygiene.non_finite_component_metric",
                    Severity:  SevError,
                    Component: comp,
                    Message:   "component metric value is non-finite (NaN/Inf); collector may have division-by-zero or invalid input",
                    Metric:    k,
                    Value:     "non-finite",
                    class:     ClassPrecondition,
                })
            }
        }
    }

    // (E) Precondition failures: required metrics presence.
    for _, m := range v.RequiredMetrics {
        if _, ok := in.Observed[m]; !ok {
            findings = append(findings, findingPrecond(
                "operability.snapshot_hygiene.missing_required_metric",
                "required observed metric is missing",
                m,
                "",
            ))
        }
    }

    sortFindings(findings)
    return finalize(findings, hasValidation)
}

func (v *SnapshotValidator) appliesTo(targetType string) bool {
    if len(v.AppliesToTargetTypes) == 0 {
        return true
    }
    tt := strings.ToLower(strings.TrimSpace(targetType))
    for _, a := range v.AppliesToTargetTypes {
        if tt == a {
            return true
        }
    }
    return false
}

func finalize(findings []Finding, hasValidation bool) Result {
    if len(findings) == 0 {
        return Result{Ok: true, Code: CodeOK, Findings: nil}
    }
    if hasValidation {
        return Result{Ok: false, Code: CodeValidationFailed, Findings: findings}
    }
    return Result{Ok: false, Code: CodePreconditionFail, Findings: findings}
}

func findingPrecond(ruleID, msg, metric, value string) Finding {
    return Finding{
        RuleID:    ruleID,
        Severity:  SevError,
        Component: "input",
        Message:   msg,
        Metric:    metric,
        Value:     value,
        class:     ClassPrecondition,
    }
}

// sortFindings enforces stable ordering: severity desc, rule_id asc, component asc, metric asc, message asc.
func sortFindings(fs []Finding) {
    sevRank := func(s Severity) int {
        switch s {
        case SevError:
            return 3
        case SevWarn:
            return 2
        case SevInfo:
            return 1
        default:
            return 0
        }
    }

    sort.Slice(fs, func(i, j int) bool {
        ri, rj := sevRank(fs[i].Severity), sevRank(fs[j].Severity)
        if ri != rj {
            return ri > rj
        }
        if fs[i].RuleID != fs[j].RuleID {
            return fs[i].RuleID < fs[j].RuleID
        }
        if fs[i].Component != fs[j].Component {
            return fs[i].Component < fs[j].Component
        }
        if fs[i].Metric != fs[j].Metric {
            return fs[i].Metric < fs[j].Metric
        }
        return fs[i].Message < fs[j].Message
    })
}

func dedupeAndSort(in []string) []string {
    if len(in) == 0 {
        return nil
    }
    m := make(map[string]struct{}, len(in))
    out := make([]string, 0, len(in))
    for _, v := range in {
        s := strings.TrimSpace(v)
        if s == "" {
            continue
        }
        if _, ok := m[s]; ok {
            continue
        }
        m[s] = struct{}{}
        out = append(out, s)
    }
    sort.Strings(out)
    return out
}

func dedupeAndSortLower(in []string) []string {
    if len(in) == 0 {
        return nil
    }
    m := make(map[string]struct{}, len(in))
    out := make([]string, 0, len(in))
    for _, v := range in {
        s := strings.ToLower(strings.TrimSpace(v))
        if s == "" {
            continue
        }
        if _, ok := m[s]; ok {
            continue
        }
        m[s] = struct{}{}
        out = append(out, s)
    }
    sort.Strings(out)
    return out
}

func containsLower(sortedLower []string, v string) bool {
    // sortedLower is sorted; use binary search.
    i := sort.SearchStrings(sortedLower, v)
    return i < len(sortedLower) && sortedLower[i] == v
}
