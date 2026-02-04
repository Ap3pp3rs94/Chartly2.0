package validators

import (
    "fmt"
    "math"
    "strings"
)

// RuleComparator defines how a metric is evaluated against a threshold.
type RuleComparator string

const (
    CompareGT  RuleComparator = "gt"
    CompareGTE RuleComparator = "gte"
    CompareLT  RuleComparator = "lt"
    CompareLTE RuleComparator = "lte"
)

// Rule defines a single deterministic validation rule.
type Rule struct {
    ID         string
    Metric     string
    Comparator RuleComparator
    Threshold  float64
    Severity   Severity
    Message    string
}

// RulesValidator evaluates explicit deterministic rules against an observed snapshot.
type RulesValidator struct {
    Rules                []Rule
    AppliesToTargetTypes []string
    AllowedTargetTypes   []string
}

// NewRulesValidator constructs a deterministic RulesValidator.
// Rules are copied and sorted by ID to guarantee stable evaluation order.
func NewRulesValidator(
    rules []Rule,
    appliesTo []string,
    allowedTargetTypes []string,
) *RulesValidator {
    // copy
    rs := make([]Rule, 0, len(rules))
    rs = append(rs, rules...)

    // stable sort by ID (in-place, deterministic)
    for i := 1; i < len(rs); i++ {
        j := i
        for j > 0 && rs[j-1].ID > rs[j].ID {
            rs[j-1], rs[j] = rs[j], rs[j-1]
            j--
        }
    }

    return &RulesValidator{
        Rules:                rs,
        AppliesToTargetTypes: dedupeAndSortLower(appliesTo),
        AllowedTargetTypes:   dedupeAndSortLower(allowedTargetTypes),
    }
}

// ID returns a stable validator identifier.
func (v *RulesValidator) ID() string {
    return "operability.rules"
}

// Run evaluates rules deterministically.
func (v *RulesValidator) Run(in Input) Result {
    if !v.appliesTo(in.TargetType) {
        return Result{Ok: true, Code: CodeNotApplicable, Findings: nil}
    }

    findings := make([]Finding, 0, len(v.Rules))

    // Validation: unsupported target type
    if len(v.AllowedTargetTypes) > 0 {
        tt := strings.ToLower(strings.TrimSpace(in.TargetType))
        if tt != "" && !containsLower(v.AllowedTargetTypes, tt) {
            findings = append(findings, Finding{
                RuleID:    "operability.rules.target_type_unsupported",
                Severity:  SevError,
                Component: "input",
                Message:   "target_type is not supported by this validator contract",
                Metric:    "target_type",
                Value:     in.TargetType,
                class:     ClassValidation,
            })
        }
    }

    // Precondition: observed snapshot required
    if in.Observed == nil {
        findings = append(findings, Finding{
            RuleID:    "operability.rules.observed_required",
            Severity:  SevError,
            Component: "input",
            Message:   "observed metrics snapshot is required",
            Metric:    "observed",
            Value:     "",
            class:     ClassPrecondition,
        })
        sortFindings(findings)
        return finalize(findings, anyValidation(findings))
    }

    for _, r := range v.Rules {
        // Rule schema validation (contract violation)
        if strings.TrimSpace(r.ID) == "" ||
            strings.TrimSpace(r.Metric) == "" ||
            strings.TrimSpace(r.Message) == "" {

            findings = append(findings, Finding{
                RuleID:    "operability.rules.invalid_rule_definition",
                Severity:  SevError,
                Component: "rule",
                Message:   "rule definition is missing required fields (id, metric, or message)",
                Metric:    r.Metric,
                Value:     "",
                class:     ClassValidation,
            })
            continue
        }

        val, ok := in.Observed[r.Metric]
        if !ok {
            findings = append(findings, Finding{
                RuleID:    r.ID + ".metric_missing",
                Severity:  SevError,
                Component: "snapshot",
                Message:   "required metric for rule is missing from snapshot",
                Metric:    r.Metric,
                Value:     "",
                class:     ClassPrecondition,
            })
            continue
        }

        if math.IsNaN(val) || math.IsInf(val, 0) {
            findings = append(findings, Finding{
                RuleID:    r.ID + ".non_finite_metric",
                Severity:  SevError,
                Component: "snapshot",
                Message:   "observed metric value is non-finite (NaN/Inf)",
                Metric:    r.Metric,
                Value:     "non-finite",
                class:     ClassPrecondition,
            })
            continue
        }

        if !isValidComparator(r.Comparator) {
            findings = append(findings, Finding{
                RuleID:    r.ID + ".invalid_comparator",
                Severity:  SevError,
                Component: "rule",
                Message:   "unsupported rule comparator",
                Metric:    r.Metric,
                Value:     string(r.Comparator),
                class:     ClassValidation,
            })
            continue
        }

        if !compare(val, r.Comparator, r.Threshold) {
            findings = append(findings, Finding{
                RuleID:    r.ID,
                Severity:  r.Severity,
                Component: "rule",
                Message:   r.Message,
                Metric:    r.Metric,
                Value:     formatFloat(val) + " (threshold=" + formatFloat(r.Threshold) + ")",
                class:     ClassValidation,
            })
        }
    }

    sortFindings(findings)
    return finalize(findings, anyValidation(findings))
}

func (v *RulesValidator) appliesTo(targetType string) bool {
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

func isValidComparator(c RuleComparator) bool {
    switch c {
    case CompareGT, CompareGTE, CompareLT, CompareLTE:
        return true
    default:
        return false
    }
}

func compare(val float64, cmp RuleComparator, threshold float64) bool {
    switch cmp {
    case CompareGT:
        return val > threshold
    case CompareGTE:
        return val >= threshold
    case CompareLT:
        return val < threshold
    case CompareLTE:
        return val <= threshold
    default:
        return false
    }
}

// anyValidation derives the "hasValidation" boolean from Finding.class,
// preventing drift from manual bookkeeping.
func anyValidation(findings []Finding) bool {
    for _, f := range findings {
        if f.class == ClassValidation {
            return true
        }
    }
    return false
}

func formatFloat(v float64) string {
    s := fmt.Sprintf("%.3f", v)
    s = strings.TrimRight(s, "0")
    s = strings.TrimRight(s, ".")
    return s
}
