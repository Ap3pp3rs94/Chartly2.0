package quarantine

import (

	"strings"
)

type RecoveryAction string

const (

	RedactAndRelease RecoveryAction = "redact_and_release"

	Drop             RecoveryAction = "drop"

	Requeue          RecoveryAction = "requeue"

	ManualReview     RecoveryAction = "manual_review"
)

type Decision struct {

	Action RecoveryAction     `json:"action"`

	Reason string             `json:"reason"`

	Notes  map[string]string  `json:"notes,omitempty"`
}

type RecoveryPolicy interface {

	Decide(e Entry) Decision
}

type DefaultRecoveryPolicy struct{}

func (DefaultRecoveryPolicy) Decide(e Entry) Decision {

	r := strings.ToLower(strings.TrimSpace(string(e.Reason)))

	switch r {

	case "pii_detected":


		return Decision{Action: RedactAndRelease, Reason: "pii_detected"}

	case "schema_violation":


		return Decision{Action: ManualReview, Reason: "schema_violation"}

	case "outlier_detected":


		return Decision{Action: ManualReview, Reason: "outlier_detected"}

	case "parse_error":


		return Decision{Action: Drop, Reason: "parse_error"}

	case "policy_denied":


		return Decision{Action: Requeue, Reason: "policy_denied"}

	default:


		return Decision{Action: ManualReview, Reason: "default"}

	}
}

func ApplyDecision(e Entry, d Decision) Entry {

	if e.Details == nil {


		e.Details = make(map[string]string)

	}

	e.Details["recovery.action"] = string(d.Action)

	e.Details["recovery.reason"] = strings.TrimSpace(d.Reason)

	return e
}
