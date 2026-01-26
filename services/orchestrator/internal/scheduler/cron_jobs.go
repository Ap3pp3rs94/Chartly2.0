package scheduler

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidCron  = errors.New("invalid cron expression")
	ErrMissingField = errors.New("missing required field")
	ErrInvalidField = errors.New("invalid field")
)

// CronJob describes a scheduled job (model + validation only; execution engine is separate).
type CronJob struct {
	Name     string `json:"name"`
	Cron     string `json:"cron"` // 5-field cron: "min hour dom mon dow"
	TenantID string `json:"tenant_id"`
	SourceID string `json:"source_id"`
	JobType  string `json:"job_type"`
	Enabled  bool   `json:"enabled"`
	Timezone string `json:"timezone"`
}

func (j CronJob) Validate() error {
	if strings.TrimSpace(j.Name) == "" {
		return fmt.Errorf("%w: name", ErrMissingField)
	}
	if strings.TrimSpace(j.Cron) == "" {
		return fmt.Errorf("%w: cron", ErrMissingField)
	}
	if strings.TrimSpace(j.TenantID) == "" {
		return fmt.Errorf("%w: tenant_id", ErrMissingField)
	}
	if strings.TrimSpace(j.SourceID) == "" {
		return fmt.Errorf("%w: source_id", ErrMissingField)
	}
	if j.Timezone != "" {
		if _, err := time.LoadLocation(j.Timezone); err != nil {
			return fmt.Errorf("%w: timezone", ErrInvalidField)
		}
	}
	if err := ValidateCronExpr(j.Cron); err != nil {
		return err
	}
	return nil
}

// ValidateCronExpr validates a 5-field cron expression: "min hour dom mon dow".
// Supported tokens per field:
// - "*" (any)
// - "*/n" (step)
// - "a-b" (range)
// - "a,b,c" (lists)
// - combinations of range/list/step where reasonable (e.g., "0,15,30,45", "1-5", "*/10")
func ValidateCronExpr(expr string) error {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return fmt.Errorf("%w: expected 5 fields", ErrInvalidCron)
	}

	ranges := [5][2]int{
		{0, 59}, // minute
		{0, 23}, // hour
		{1, 31}, // dom
		{1, 12}, // month
		{0, 6},  // dow (0=Sun)
	}

	for i := 0; i < 5; i++ {
		if err := validateField(fields[i], ranges[i][0], ranges[i][1]); err != nil {
			return fmt.Errorf("%w: field %d: %v", ErrInvalidCron, i, err)
		}
	}
	return nil
}

func validateField(field string, min, max int) error {
	field = strings.TrimSpace(field)
	if field == "" {
		return fmt.Errorf("%w: empty", ErrMissingField)
	}
	if field == "*" {
		return nil
	}

	// step: */n
	if strings.HasPrefix(field, "*/") {
		n, err := strconv.Atoi(strings.TrimPrefix(field, "*/"))
		if err != nil || n <= 0 {
			return fmt.Errorf("%w: invalid step", ErrInvalidField)
		}
		return nil
	}

	// list: a,b,c OR mix including ranges
	parts := strings.Split(field, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return fmt.Errorf("%w: empty list item", ErrInvalidField)
		}
		if p == "*" {
			continue
		}
		if strings.HasPrefix(p, "*/") {
			n, err := strconv.Atoi(strings.TrimPrefix(p, "*/"))
			if err != nil || n <= 0 {
				return fmt.Errorf("%w: invalid step", ErrInvalidField)
			}
			continue
		}
		if strings.Contains(p, "-") {
			bits := strings.Split(p, "-")
			if len(bits) != 2 {
				return fmt.Errorf("%w: invalid range", ErrInvalidField)
			}
			a, err1 := strconv.Atoi(strings.TrimSpace(bits[0]))
			b, err2 := strconv.Atoi(strings.TrimSpace(bits[1]))
			if err1 != nil || err2 != nil {
				return fmt.Errorf("%w: invalid range number", ErrInvalidField)
			}
			if a > b {
				return fmt.Errorf("%w: range start > end", ErrInvalidField)
			}
			if a < min || b > max {
				return fmt.Errorf("%w: range out of bounds", ErrInvalidField)
			}
			continue
		}

		// number
		n, err := strconv.Atoi(p)
		if err != nil {
			return fmt.Errorf("%w: invalid number", ErrInvalidField)
		}
		if n < min || n > max {
			return fmt.Errorf("%w: number out of bounds", ErrInvalidField)
		}
	}
	return nil
}

// NextRun computes the next run time after "now" for expr in loc.
// Brute-force minute stepping up to 366 days ahead (bounded).
func NextRun(now time.Time, expr string, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.UTC
	}
	if err := ValidateCronExpr(expr); err != nil {
		return time.Time{}, err
	}

	fields := strings.Fields(strings.TrimSpace(expr))
	minF, hourF, domF, monF, dowF := fields[0], fields[1], fields[2], fields[3], fields[4]

	// Start at next minute boundary in requested timezone
	t := now.In(loc).Truncate(time.Minute).Add(time.Minute)

	deadline := t.Add(366 * 24 * time.Hour)
	for !t.After(deadline) {
		if matchField(t.Minute(), minF) &&
			matchField(t.Hour(), hourF) &&
			matchField(t.Day(), domF) &&
			matchField(int(t.Month()), monF) &&
			matchField(int(t.Weekday()), dowF) {
			return t, nil
		}
		t = t.Add(time.Minute)
	}

	return time.Time{}, fmt.Errorf("%w: no run found within 366 days", ErrInvalidCron)
}

func matchField(value int, field string) bool {
	field = strings.TrimSpace(field)
	if field == "*" {
		return true
	}
	if strings.HasPrefix(field, "*/") {
		n, err := strconv.Atoi(strings.TrimPrefix(field, "*/"))
		if err != nil || n <= 0 {
			return false
		}
		return value%n == 0
	}
	parts := strings.Split(field, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if p == "*" {
			return true
		}
		if strings.HasPrefix(p, "*/") {
			n, err := strconv.Atoi(strings.TrimPrefix(p, "*/"))
			if err == nil && n > 0 && value%n == 0 {
				return true
			}
			continue
		}
		if strings.Contains(p, "-") {
			bits := strings.Split(p, "-")
			if len(bits) != 2 {
				continue
			}
			a, err1 := strconv.Atoi(strings.TrimSpace(bits[0]))
			b, err2 := strconv.Atoi(strings.TrimSpace(bits[1]))
			if err1 != nil || err2 != nil {
				continue
			}
			if value >= a && value <= b {
				return true
			}
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		if value == n {
			return true
		}
	}
	return false
}
