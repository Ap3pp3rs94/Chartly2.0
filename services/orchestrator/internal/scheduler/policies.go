package scheduler

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrPolicyInvalid = errors.New("policy invalid")
)

type QuietHours struct {
	Enabled   bool   `json:"enabled"`
	Timezone  string `json:"timezone"` // default America/Chicago
	StartHHMM string `json:"start_hhmm"`
	EndHHMM   string `json:"end_hhmm"`
	Mode      string `json:"mode"` // "deny" | "allow"
}
type Policy struct {
	Enabled                  bool       `json:"enabled"`
	MaxTriggersPerMinute     int        `json:"max_triggers_per_minute"`
	MaxTriggersPerJobPerHour int        `json:"max_triggers_per_job_per_hour"`
	AllowJobTypes            []string   `json:"allow_job_types"`
	DenyTenants              []string   `json:"deny_tenants"`
	AllowTenants             []string   `json:"allow_tenants"`
	QuietHours               QuietHours `json:"quiet_hours"`
}
type Decision struct {
	Allowed bool              `json:"allowed"`
	Reason  string            `json:"reason"`
	Tags    map[string]string `json:"tags,omitempty"`
}

func DefaultPolicy() Policy {
	return Policy{
		Enabled:                  true,
		MaxTriggersPerMinute:     60,
		MaxTriggersPerJobPerHour: 12,
		AllowJobTypes:            []string{"ingest"},
		DenyTenants:              nil,
		AllowTenants:             nil,
		QuietHours: QuietHours{
			Enabled:   false,
			Timezone:  "America/Chicago",
			StartHHMM: "22:00",
			EndHHMM:   "06:00",
			Mode:      "deny",
		},
	}
}
func (p Policy) Validate() error {
	if p.MaxTriggersPerMinute <= 0 {
		return fmt.Errorf("%w: max_triggers_per_minute", ErrPolicyInvalid)
	}
	if p.MaxTriggersPerJobPerHour <= 0 {
		return fmt.Errorf("%w: max_triggers_per_job_per_hour", ErrPolicyInvalid)
	}
	if len(p.AllowJobTypes) == 0 {
		return fmt.Errorf("%w: allow_job_types empty", ErrPolicyInvalid)
	}
	if p.QuietHours.Enabled {
		if strings.TrimSpace(p.QuietHours.Timezone) == "" {
			return fmt.Errorf("%w: quiet_hours.timezone", ErrPolicyInvalid)
		}
		if _, err := time.LoadLocation(p.QuietHours.Timezone); err != nil {
			return fmt.Errorf("%w: quiet_hours.timezone", ErrPolicyInvalid)
		}
		if _, err := parseHHMM(p.QuietHours.StartHHMM); err != nil {
			return fmt.Errorf("%w: quiet_hours.start_hhmm", ErrPolicyInvalid)
		}
		if _, err := parseHHMM(p.QuietHours.EndHHMM); err != nil {
			return fmt.Errorf("%w: quiet_hours.end_hhmm", ErrPolicyInvalid)
		}
		mode := strings.ToLower(strings.TrimSpace(p.QuietHours.Mode))
		if mode != "deny" && mode != "allow" {
			return fmt.Errorf("%w: quiet_hours.mode", ErrPolicyInvalid)
		}
	}
	return nil
}

// Decide returns a deterministic policy decision (no side effects).
func (p *Policy) Decide(now time.Time, tenantID string, job CronJob) Decision {
	// Disabled policy = allow
	if p == nil || !p.Enabled {
		return Decision{Allowed: true, Reason: "policy_disabled"}
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return Decision{Allowed: false, Reason: "missing_tenant"}
	}

	// Denylist wins
	for _, t := range p.DenyTenants {
		if strings.EqualFold(strings.TrimSpace(t), tenantID) {
			return Decision{Allowed: false, Reason: "tenant_denied"}
		}
	}

	// Allowlist if present
	if len(p.AllowTenants) > 0 {
		ok := false
		for _, t := range p.AllowTenants {
			if strings.EqualFold(strings.TrimSpace(t), tenantID) {
				ok = true
				break
			}
		}
		if !ok {
			return Decision{Allowed: false, Reason: "tenant_not_allowed"}
		}
	}

	// Job type allowlist
	jt := strings.TrimSpace(job.JobType)
	if jt == "" {
		jt = "ingest"
	}
	allowedType := false
	for _, t := range p.AllowJobTypes {
		if strings.EqualFold(strings.TrimSpace(t), jt) {
			allowedType = true
			break
		}
	}
	if !allowedType {
		return Decision{Allowed: false, Reason: "job_type_not_allowed"}
	}

	// Quiet hours
	if p.QuietHours.Enabled {
		dec := quietHoursDecision(p.QuietHours, now)
		if !dec.Allowed {
			dec.Tags = map[string]string{
				"quiet_hours": "true",
			}
			return dec
		}
	}
	return Decision{Allowed: true, Reason: "allowed"}
}
func quietHoursDecision(q QuietHours, now time.Time) Decision {
	loc := time.UTC
	if tz := strings.TrimSpace(q.Timezone); tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	mode := strings.ToLower(strings.TrimSpace(q.Mode))
	startMin, _ := parseHHMM(q.StartHHMM)
	endMin, _ := parseHHMM(q.EndHHMM)
	t := now.In(loc)
	minOfDay := t.Hour()
	*60 + t.Minute()
	inQuiet := isInWindow(minOfDay, startMin, endMin)
	switch mode {
	case "deny":
		if inQuiet {
			return Decision{Allowed: false, Reason: "quiet_hours_deny"}
		}
		return Decision{Allowed: true, Reason: "outside_quiet_hours"}
	case "allow":
		if inQuiet {
			return Decision{Allowed: true, Reason: "quiet_hours_allow"}
		}
		return Decision{Allowed: false, Reason: "outside_quiet_hours"}
	default:
		return Decision{Allowed: false, Reason: "quiet_hours_mode_invalid"}
	}
}

// isInWindow handles windows that wrap midnight.
// start=end means "full day" is considered in-window.
func isInWindow(minute, start, end int) bool {
	if start == end {
		return true
	}
	if start < end {
		return minute >= start && minute < end
	}
	// wrap midnight
	return minute >= start || minute < end
}
func parseHHMM(s string) (int, error) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("%w: invalid hh:mm", ErrPolicyInvalid)
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, fmt.Errorf("%w: invalid hh:mm", ErrPolicyInvalid)
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("%w: hh:mm out of range", ErrPolicyInvalid)
	}
	return h*60 + m, nil
}

////////////////////////////////////////////////////////////////////////////////
// In-memory limiter (concurrency-safe, bounded memory, prunes old keys)
////////////////////////////////////////////////////////////////////////////////

// type minuteKey int64
// type hourKey string

type Limiter struct {
	p Policy

	mu sync.Mutex

	// per-minute global counter
	minuteCounts map[minuteKey]int

	// per-job-per-hour counter (tenant+jobName)
	perJobHourCounts map[hourKey]int

	// last prune markers
	// lastPrunedMinute int64
	// lastPrunedHour   int64
}

func NewLimiter(p Policy) *Limiter {
	return &Limiter{
		p:                p,
		minuteCounts:     make(map[minuteKey]int),
		perJobHourCounts: make(map[hourKey]int),
		lastPrunedMinute: 0,
		lastPrunedHour:   0,
	}
}

// Allow checks and increments counters if allowed.
func (l *Limiter) Allow(now time.Time, tenantID string, jobName string) (ok bool, reason string) {
	if !l.p.Enabled {
		return true, "policy_disabled"
	}
	m := now.Unix() / 60
	h := now.Unix() / 3600

	l.mu.Lock()
	defer l.mu.Unlock()

	// Prune occasionally to keep memory bounded.
	l.pruneLocked(m, h)

	// Global per-minute limit
	mk := minuteKey(m)
	if l.minuteCounts[mk] >= l.p.MaxTriggersPerMinute {
		return false, "rate_limited_minute"
	}

	// Per job per hour limit
	hk := hourKey(fmt.Sprintf("%s|%s|%d", strings.ToLower(tenantID), strings.ToLower(jobName), h))
	if l.perJobHourCounts[hk] >= l.p.MaxTriggersPerJobPerHour {
		return false, "rate_limited_job_hour"
	}
	l.minuteCounts[mk]++
	l.perJobHourCounts[hk]++
	return true, "allowed"
}

// ResetOld prunes old keys based on "now".
func (l *Limiter) ResetOld(now time.Time) {
	m := now.Unix() / 60
	h := now.Unix() / 3600

	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(m, h)
}
func (l *Limiter) pruneLocked(curMinute int64, curHour int64) {
	// Keep last 5 minutes
	if l.lastPrunedMinute == 0 || curMinute-l.lastPrunedMinute >= 2 {
		for k := range l.minuteCounts {
			if int64(k) < curMinute-5 {
				delete(l.minuteCounts, k)
			}
		}
		l.lastPrunedMinute = curMinute
	}

	// Keep last 2 hours worth of per-job counters
	if l.lastPrunedHour == 0 || curHour-l.lastPrunedHour >= 1 {
		for k := range l.perJobHourCounts {
			parts := strings.Split(string(k), "|")
			if len(parts) < 3 {
				delete(l.perJobHourCounts, k)
				continue
			}
			hStr := parts[len(parts)-1]
			hv, err := strconv.ParseInt(hStr, 10, 64)
			if err != nil || hv < curHour-2 {
				delete(l.perJobHourCounts, k)
			}
		}
		l.lastPrunedHour = curHour
	}
}
