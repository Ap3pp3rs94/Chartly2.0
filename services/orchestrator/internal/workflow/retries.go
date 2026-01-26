package workflow

import (
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"strings"
	"time"
)

var (
	ErrRetryInvalid = errors.New("retry policy invalid")
)

type Policy struct {
	Enabled      bool          `json:"enabled"`
	MaxAttempts  int           `json:"max_attempts"`
	InitialDelay time.Duration `json:"initial_delay"`
	MaxDelay     time.Duration `json:"max_delay"`
	Multiplier   float64       `json:"multiplier"`
	JitterPct    float64       `json:"jitter_pct"`
}

func DefaultRetryPolicy() Policy {
	return Policy{
		Enabled:      true,
		MaxAttempts:  5,
		InitialDelay: 250 * time.Millisecond,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		JitterPct:    0.0, // deterministic by default
	}
}

func (p Policy) Validate() error {
	if p.MaxAttempts < 0 {
		return fmt.Errorf("%w: max_attempts", ErrRetryInvalid)
	}
	if p.InitialDelay < 0 {
		return fmt.Errorf("%w: initial_delay", ErrRetryInvalid)
	}
	if p.MaxDelay < 0 {
		return fmt.Errorf("%w: max_delay", ErrRetryInvalid)
	}
	if p.Multiplier < 1.0 && p.MaxAttempts > 0 {
		return fmt.Errorf("%w: multiplier", ErrRetryInvalid)
	}
	if p.JitterPct < 0 || p.JitterPct >= 1.0 {
		return fmt.Errorf("%w: jitter_pct", ErrRetryInvalid)
	}
	if p.MaxDelay > 0 && p.InitialDelay > p.MaxDelay {
		return fmt.Errorf("%w: initial_delay > max_delay", ErrRetryInvalid)
	}
	return nil
}

// Next computes the delay for a given retry attempt (1-based).
func (p Policy) Next(jobID string, attempt int) (delay time.Duration, ok bool, reason string) {
	if !p.Enabled {
		return 0, false, "disabled"
	}
	if attempt <= 0 {
		return 0, false, "invalid_attempt"
	}
	if p.MaxAttempts > 0 && attempt > p.MaxAttempts {
		return 0, false, "max_attempts_exceeded"
	}
	if err := p.Validate(); err != nil {
		return 0, false, "invalid_policy"
	}

	// Base exponential: initial * multiplier^(attempt-1)
	base := float64(p.InitialDelay)
	if base <= 0 {
		base = float64(250 * time.Millisecond)
	}
	mult := p.Multiplier
	if mult < 1.0 {
		mult = 2.0
	}

	exp := math.Pow(mult, float64(attempt-1))
	raw := time.Duration(base * exp)

	maxD := p.MaxDelay
	if maxD <= 0 {
		maxD = 30 * time.Second
	}
	if raw > maxD {
		raw = maxD
	}

	if p.JitterPct <= 0 {
		return raw, true, "ok"
	}

	// Deterministic jitter: FNV-1a(jobID:attempt) -> u in [0,1)
	jid := strings.TrimSpace(jobID)
	if jid == "" {
		jid = "unknown"
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(jid))
	_, _ = h.Write([]byte(":"))
	_, _ = h.Write([]byte(fmt.Sprintf("%d", attempt)))
	sum := h.Sum64()

	// Convert to [0,1)
	u := float64(sum%1000000) / 1000000.0

	// Map u to [-1, +1]
	x := (u * 2.0) - 1.0
	j := 1.0 + (x * p.JitterPct)

	jittered := time.Duration(float64(raw) * j)
	if jittered < 0 {
		jittered = 0
	}
	if jittered > maxD {
		jittered = maxD
	}

	return jittered, true, "ok_jittered"
}
