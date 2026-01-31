package ledger

// Verification helpers for audit events and hash-chain integrity (deterministic).
//
// This file provides utilities that higher layers can use to validate audit event sets and
// compute/verify the tamper-evident hash chain.
//
// Determinism guarantees:
//   - No randomness.
//   - No time.Now usage.
//   - Explicit stable sorting (TS asc, EventID asc).
//   - Duplicate detection uses deterministic keys.

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	ErrVerify          = errors.New("verify failed")
	ErrVerifyInvalid   = errors.New("verify invalid")
	ErrVerifyDuplicate = errors.New("verify duplicate")
	ErrVerifyMismatch  = errors.New("verify mismatch")
)

type VerifyOptions struct {
	EnforceTenantID    bool
	RequireMonotonicTS bool
	MaxEvents          int
}

func VerifyEvents(tenantID string, events []Event, opts VerifyOptions) error {
	o := normalizeVerifyOptions(opts)

	if len(events) == 0 {
		return fmt.Errorf("%w: %w: no events", ErrVerify, ErrVerifyInvalid)
	}

	if o.MaxEvents > 0 && len(events) > o.MaxEvents {
		return fmt.Errorf("%w: too many events (%d>%d)", ErrVerifyInvalid, len(events), o.MaxEvents)
	}

	tid := normCollapse(tenantID)
	if tid == "" {
		tid = normCollapse(events[0].TenantID)
	}
	if o.EnforceTenantID && tid == "" {
		return fmt.Errorf("%w: %w: tenant_id required", ErrVerify, ErrVerifyInvalid)
	}

	// Validate and build a deterministic duplicate set.
	seen := make(map[string]struct{}, len(events))

	for i := range events {
		ev := events[i]
		evT := normCollapse(ev.TenantID)
		evID := normCollapse(ev.EventID)
		evTS := normCollapse(ev.TS)
		evAct := normCollapse(ev.Action)
		evOut := normCollapse(ev.Outcome)

		if o.EnforceTenantID {
			if evT == "" {
				return fmt.Errorf("%w: %w: missing tenant_id at index %d", ErrVerify, ErrVerifyInvalid, i)
			}
			if tid != "" && evT != tid {
				return fmt.Errorf("%w: %w: tenant_id mismatch at index %d", ErrVerify, ErrVerifyInvalid, i)
			}
		}

		if evID == "" || evTS == "" || evAct == "" || evOut == "" {
			return fmt.Errorf("%w: %w: missing required fields at index %d", ErrVerify, ErrVerifyInvalid, i)
		}

		if _, err := parseRFC3339Strict(evTS); err != nil {
			return fmt.Errorf("%w: %w: invalid ts at index %d", ErrVerify, ErrVerifyInvalid, i)
		}

		// Deterministic duplicate key: <tenant>|<event_id>
		key := evT + "|" + evID
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: tenant=%s event_id=%s", ErrVerifyDuplicate, evT, evID)
		}
		seen[key] = struct{}{}
	}

	if o.RequireMonotonicTS {
		// Sort a view deterministically and ensure TS is non-decreasing.
		type item struct {
			ts time.Time
			id string
		}
		view := make([]item, 0, len(events))
		for _, ev := range events {
			t, _ := parseRFC3339Strict(normCollapse(ev.TS))
			view = append(view, item{ts: t, id: normCollapse(ev.EventID)})
		}
		sort.Slice(view, func(i, j int) bool {
			if view[i].ts.Before(view[j].ts) {
				return true
			}
			if view[i].ts.After(view[j].ts) {
				return false
			}
			return view[i].id < view[j].id
		})
		for i := 1; i < len(view); i++ {
			if view[i].ts.Before(view[i-1].ts) {
				return fmt.Errorf("%w: %w: timestamps not monotonic", ErrVerify, ErrVerifyMismatch)
			}
		}
	}

	return nil
}

func VerifyLedgerSnapshot(tenantID string, events []Event, opts VerifyOptions) (Chain, error) {
	if err := VerifyEvents(tenantID, events, opts); err != nil {
		return Chain{}, err
	}

	tid := normCollapse(tenantID)
	if tid == "" {
		tid = normCollapse(events[0].TenantID)
	}

	ch, err := BuildChain(tid, events)
	if err != nil {
		return Chain{}, err
	}

	return ch, nil
}

func VerifyChainMatches(tenantID string, chain Chain, events []Event, opts VerifyOptions) error {
	if err := VerifyEvents(tenantID, events, opts); err != nil {
		return err
	}

	tid := normCollapse(tenantID)
	if tid == "" {
		tid = normCollapse(chain.TenantID)
	}
	if tid == "" {
		tid = normCollapse(events[0].TenantID)
	}
	if tid == "" {
		return fmt.Errorf("%w: %w: tenant_id required", ErrVerify, ErrVerifyInvalid)
	}

	if err := VerifyChain(chain, events); err != nil {
		// Normalize to verification sentinel.
		return fmt.Errorf("%w: %w: %v", ErrVerify, ErrVerifyMismatch, err)
	}

	// Ensure tenant matches if enforcing.
	if normalizeVerifyOptions(opts).EnforceTenantID {
		if normCollapse(chain.TenantID) != tid {
			return fmt.Errorf("%w: %w: chain tenant_id mismatch", ErrVerify, ErrVerifyMismatch)
		}
	}

	return nil
}

func normalizeVerifyOptions(opts VerifyOptions) VerifyOptions {
	o := opts
	// Default enforce tenant id true
	if !o.EnforceTenantID {
		// keep as provided
	} else {
		o.EnforceTenantID = true
	}
	if o.MaxEvents <= 0 {
		o.MaxEvents = 200000
	}
	return o
}
