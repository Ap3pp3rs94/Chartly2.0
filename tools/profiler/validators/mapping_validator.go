package validators

import (
    "math"
    "sort"
    "strings"
)

// MappingValidator enforces that an observed snapshot contains the signals required by a
// declared contract mapping, and that mapped metrics are finite.
//
// It relies on shared validator types/helpers defined in profile_validator.go.
// This avoids duplicate type definitions and ensures package-level compilation.
type MappingValidator struct {
    // SignalMap maps normalized contract signal name -> observed metric key.
    // Signal names are normalized (trim + lower) at construction time.
    // Observed metric keys are trimmed (case preserved).
    SignalMap map[string]string

    // RequiredSignals are normalized contract signal names that must be resolvable
    // via SignalMap and present in the observed snapshot.
    RequiredSignals []string

    // AppliesToTargetTypes is an allowlist. If empty, applies to all targets.
    AppliesToTargetTypes []string

    // AllowedTargetTypes defines contractually allowed target types.
    // If non-empty and target_type is not allowed, this is a validation failure.
    AllowedTargetTypes []string
}

// NewMappingValidator constructs a deterministic MappingValidator.
//
// Normalization rules (binding):
// - contract signal names: trim + lower
// - observed metric keys: trim
// - RequiredSignals: normalized, deduped, sorted
// - allowlists: normalized (lower), deduped, sorted
func NewMappingValidator(
    signalMap map[string]string,
    requiredSignals []string,
    appliesTo []string,
    allowedTargetTypes []string,
) *MappingValidator {
    normMap := make(map[string]string, len(signalMap))
    for k, v := range signalMap {
        sig := strings.ToLower(strings.TrimSpace(k))
        obs := strings.TrimSpace(v)
        // Preserve empty values so validator can flag them deterministically.
        normMap[sig] = obs
    }

    return &MappingValidator{
        SignalMap:            normMap,
        RequiredSignals:      dedupeAndSortLower(requiredSignals),
        AppliesToTargetTypes: dedupeAndSortLower(appliesTo),
        AllowedTargetTypes:   dedupeAndSortLower(allowedTargetTypes),
    }
}

// ID returns a stable, globally unique validator ID.
func (v *MappingValidator) ID() string { return "operability.signal_mapping" }

// Run validates snapshot mappings deterministically.
func (v *MappingValidator) Run(in Input) Result {
    if !v.appliesTo(in.TargetType) {
        return Result{Ok: true, Code: CodeNotApplicable, Findings: nil}
    }

    findings := make([]Finding, 0, 24)
    hasValidation := false

    // (A) Validation failure: unsupported target type.
    if len(v.AllowedTargetTypes) > 0 {
        tt := strings.ToLower(strings.TrimSpace(in.TargetType))
        if tt != "" && !containsLower(v.AllowedTargetTypes, tt) {
            findings = append(findings, Finding{
                RuleID:    "operability.signal_mapping.target_type_unsupported",
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

    // (B) Precondition: observed snapshot must exist.
    if in.Observed == nil {
        findings = append(findings, Finding{
            RuleID:    "operability.signal_mapping.observed_required",
            Severity:  SevError,
            Component: "input",
            Message:   "observed metrics snapshot is required",
            Metric:    "observed",
            Value:     "",
            class:     ClassPrecondition,
        })
        sortFindings(findings)
        return finalize(findings, hasValidation)
    }

    // (C) Precondition: float hygiene on observed metrics.
    for k, val := range in.Observed {
        if math.IsNaN(val) || math.IsInf(val, 0) {
            findings = append(findings, Finding{
                RuleID:    "operability.signal_mapping.non_finite_metric",
                Severity:  SevError,
                Component: "snapshot",
                Message:   "observed metric value is non-finite (NaN/Inf); collector may have division-by-zero or invalid input",
                Metric:    k,
                Value:     "non-finite",
                class:     ClassPrecondition,
            })
        }
    }

    // (D) Validate required signal mappings (stable order).
    for _, sig := range v.RequiredSignals {
        obsKey, ok := v.SignalMap[sig]
        if !ok || strings.TrimSpace(obsKey) == "" {
            // Missing contract mapping is a validation failure.
            findings = append(findings, Finding{
                RuleID:    "operability.signal_mapping.missing_mapping",
                Severity:  SevError,
                Component: "mapping",
                Message:   "required contract signal has no mapping to an observed metric key",
                Metric:    sig,
                Value:     "",
                class:     ClassValidation,
            })
            hasValidation = true
            continue
        }

        // Mapped observed key must exist.
        if _, present := in.Observed[obsKey]; !present {
            findings = append(findings, Finding{
                RuleID:    "operability.signal_mapping.missing_observed_metric",
                Severity:  SevError,
                Component: "snapshot",
                Message:   "mapped observed metric key is missing from snapshot",
                Metric:    obsKey,
                Value:     "required_by:" + sig,
                class:     ClassPrecondition,
            })
        }
    }

    // (E) Contract hygiene: validate SignalMap entries themselves (deterministic).
    mapKeys := make([]string, 0, len(v.SignalMap))
    for k := range v.SignalMap {
        mapKeys = append(mapKeys, k)
    }
    sort.Strings(mapKeys)

    for _, sig := range mapKeys {
        obsKey := v.SignalMap[sig]
        if strings.TrimSpace(sig) == "" {
            findings = append(findings, Finding{
                RuleID:    "operability.signal_mapping.invalid_contract_signal_name",
                Severity:  SevError,
                Component: "mapping",
                Message:   "contract signal name must not be empty",
                Metric:    "signal",
                Value:     "",
                class:     ClassValidation,
            })
            hasValidation = true
        }
        if strings.TrimSpace(obsKey) == "" {
            findings = append(findings, Finding{
                RuleID:    "operability.signal_mapping.invalid_observed_key",
                Severity:  SevError,
                Component: "mapping",
                Message:   "observed metric key must not be empty/whitespace",
                Metric:    sig,
                Value:     "",
                class:     ClassValidation,
            })
            hasValidation = true
        }
    }

    sortFindings(findings)
    return finalize(findings, hasValidation)
}

func (v *MappingValidator) appliesTo(targetType string) bool {
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
