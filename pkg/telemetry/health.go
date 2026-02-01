package telemetry

import (
"bytes"
"crypto/sha256"
"encoding/hex"
"encoding/json"
"errors"
"fmt"
"sort"
"strings"
"time"
)

type Status string

const (
StatusOK       Status = "ok"
StatusDegraded Status = "degraded"
StatusFatal    Status = "fatal"
StatusUnknown  Status = "unknown"
)

const (
MaxComponents   = 64
MaxMessageLen   = 256
MaxDetails      = 32
MaxDetailKeyLen = 64
MaxDetailValLen = 256
MaxServiceLen   = 64
MaxEnvLen       = 32
MaxTenantLen    = 64

// Bounded warnings to prevent report bloat.
MaxWarnings = 32
)

var (
ErrInvalidHealth = errors.New("telemetry: invalid health")
)

// HealthWarning captures non-fatal normalization decisions (truncations, dedupe, drops).
type HealthWarning struct {
Code    string `json:"code"`
Subject string `json:"subject,omitempty"` // component name or field
Message string `json:"message"`
}

// ComponentStatus describes a single subsystem check.
type ComponentStatus struct {
Name      string            `json:"name"`
Status    Status            `json:"status"`
CheckedAt time.Time         `json:"checked_at"`
Message   string            `json:"message,omitempty"`
Details   map[string]string `json:"details,omitempty"`
}

// HealthSnapshot is the full health document emitted by a service.
type HealthSnapshot struct {
Service     string            `json:"service"`
Env         string            `json:"env,omitempty"`
Tenant      string            `json:"tenant,omitempty"`
GeneratedAt time.Time         `json:"generated_at"`
Overall     Status            `json:"overall"`
Components  []ComponentStatus `json:"components"`
Hash        string            `json:"hash"`

// Optional warnings emitted during Normalize (e.g. truncation, dedupe).
Warnings []HealthWarning `json:"warnings,omitempty"`
}

// NewHealthSnapshot builds a normalized + validated snapshot.
// Hybrid time: if now is zero, uses time.Now().UTC().
func NewHealthSnapshot(service, env, tenant string, comps []ComponentStatus, now time.Time) (HealthSnapshot, error) {
if now.IsZero() {
now = time.Now().UTC()
} else {
now = now.UTC()

}s := HealthSnapshot{
Service:     strings.TrimSpace(service),
Env:         strings.TrimSpace(env),
Tenant:      strings.TrimSpace(tenant),
GeneratedAt: now,
Components:  comps,
Overall:     StatusUnknown, // computed in Normalize()

}if err := s.Normalize(); err != nil {
return HealthSnapshot{}, err

}if err := s.Validate(); err != nil {
return HealthSnapshot{}, err

}h, err := s.StableHash()
if err != nil {
return HealthSnapshot{}, err

}s.Hash = h
return s, nil
}

// Normalize enforces deterministic ordering and bounds-friendly shaping, emitting warnings on:
// - truncations
// - dropped invalid detail entries
// - duplicate component names (deduped deterministically)
func (s *HealthSnapshot) Normalize() error {
s.Warnings = nil

s.Service = strings.TrimSpace(s.Service)
s.Env = strings.TrimSpace(s.Env)
s.Tenant = strings.TrimSpace(s.Tenant)

if len(s.Service) > MaxServiceLen {
s.warn("truncate.service", "service", fmt.Sprintf("service truncated to %d bytes", MaxServiceLen))
s.Service = s.Service[:MaxServiceLen]

}if len(s.Env) > MaxEnvLen {
s.warn("truncate.env", "env", fmt.Sprintf("env truncated to %d bytes", MaxEnvLen))
s.Env = s.Env[:MaxEnvLen]

}if len(s.Tenant) > MaxTenantLen {
s.warn("truncate.tenant", "tenant", fmt.Sprintf("tenant truncated to %d bytes", MaxTenantLen))
s.Tenant = s.Tenant[:MaxTenantLen]


}if s.GeneratedAt.IsZero() {
s.GeneratedAt = time.Now().UTC()
} else {
s.GeneratedAt = s.GeneratedAt.UTC()


}// Cap components; deterministic selection by name.
if len(s.Components) > MaxComponents {
tmp := append([]ComponentStatus(nil), s.Components...)
sort.SliceStable(tmp, func(i, j int) bool {
return strings.ToLower(strings.TrimSpace(tmp[i].Name)) < strings.ToLower(strings.TrimSpace(tmp[j].Name))
})
s.warn("truncate.components", "components", fmt.Sprintf("components truncated to %d entries", MaxComponents))
s.Components = tmp[:MaxComponents]


}// Normalize each component; then sort components by name deterministically.
for i := range s.Components {
c := &s.Components[i]
c.Name = strings.TrimSpace(c.Name)
c.Message = strings.TrimSpace(c.Message)

if len(c.Name) > MaxServiceLen {
s.warn("truncate.component_name", c.Name, fmt.Sprintf("component name truncated to %d bytes", MaxServiceLen))
c.Name = c.Name[:MaxServiceLen]

}if len(c.Message) > MaxMessageLen {
s.warn("truncate.component_message", c.Name, fmt.Sprintf("component message truncated to %d bytes", MaxMessageLen))
c.Message = c.Message[:MaxMessageLen]


}if c.CheckedAt.IsZero() {
c.CheckedAt = s.GeneratedAt
} else {
c.CheckedAt = c.CheckedAt.UTC()


}c.Status = normalizeStatus(c.Status)

// Normalize details (lowercase keys, bounded, deterministic)
if c.Details != nil {
keys := make([]string, 0, len(c.Details))
for k := range c.Details {
keys = append(keys, k)

}sort.Strings(keys)

clean := make(map[string]string, len(c.Details))
for _, k := range keys {
k2 := strings.ToLower(strings.TrimSpace(k))
if k2 == "" || len(k2) > MaxDetailKeyLen || hasCtl(k2) {
s.warn("drop.detail_key", c.Name, "dropped invalid detail key")
continue

}v := strings.TrimSpace(c.Details[k])
if hasCtl(v) {
s.warn("drop.detail_value_ctl", c.Name, "dropped detail value containing control chars")
continue

}if len(v) > MaxDetailValLen {
s.warn("truncate.detail_value", c.Name, fmt.Sprintf("detail value truncated to %d bytes", MaxDetailValLen))
v = v[:MaxDetailValLen]

}clean[k2] = v
if len(clean) >= MaxDetails {
s.warn("truncate.details", c.Name, fmt.Sprintf("details truncated to %d entries", MaxDetails))
break

}
}if len(clean) == 0 {
c.Details = nil
} else {
c.Details = clean

}
}

}sort.SliceStable(s.Components, func(i, j int) bool {
ai := strings.ToLower(strings.TrimSpace(s.Components[i].Name))
aj := strings.ToLower(strings.TrimSpace(s.Components[j].Name))
if ai != aj {
return ai < aj

}// Tie-breaker: worse status first
return statusRank(s.Components[i].Status) > statusRank(s.Components[j].Status)
})

// Deduplicate component names deterministically (keep first after sort).
if len(s.Components) > 1 {
out := make([]ComponentStatus, 0, len(s.Components))
seen := make(map[string]bool, len(s.Components))
for _, c := range s.Components {
key := strings.ToLower(strings.TrimSpace(c.Name))
if key == "" {
continue

}if seen[key] {
s.warn("dedupe.component", c.Name, "duplicate component name deduped (kept first)")
continue

}seen[key] = true
out = append(out, c)

}s.Components = out


}// Compute overall as worst component status.
overall := StatusUnknown
for i := range s.Components {
if statusRank(s.Components[i].Status) > statusRank(overall) {
overall = s.Components[i].Status

}
}s.Overall = normalizeStatus(overall)

// Bound warnings
if len(s.Warnings) > MaxWarnings {
s.Warnings = s.Warnings[:MaxWarnings]


}return nil
}

func (s *HealthSnapshot) warn(code, subject, msg string) {
if len(s.Warnings) >= MaxWarnings {
return

}s.Warnings = append(s.Warnings, HealthWarning{
Code:    strings.TrimSpace(code),
Subject: strings.TrimSpace(subject),
Message: strings.TrimSpace(msg),
})
}

func (s HealthSnapshot) Validate() error {
if strings.TrimSpace(s.Service) == "" {
return fmt.Errorf("%w: service required", ErrInvalidHealth)

}if len(s.Service) > MaxServiceLen {
return fmt.Errorf("%w: service too long", ErrInvalidHealth)

}if len(s.Env) > MaxEnvLen {
return fmt.Errorf("%w: env too long", ErrInvalidHealth)

}if len(s.Tenant) > MaxTenantLen {
return fmt.Errorf("%w: tenant too long", ErrInvalidHealth)

}if s.GeneratedAt.IsZero() {
return fmt.Errorf("%w: generated_at required", ErrInvalidHealth)

}if len(s.Warnings) > MaxWarnings {
return fmt.Errorf("%w: too many warnings", ErrInvalidHealth)


}if len(s.Components) == 0 {
if normalizeStatus(s.Overall) != StatusUnknown {
return fmt.Errorf("%w: overall must be unknown when no components", ErrInvalidHealth)

}return nil

}if len(s.Components) > MaxComponents {
return fmt.Errorf("%w: too many components", ErrInvalidHealth)


}// Ensure component names are non-empty and unique (post-normalize they should be).
seen := make(map[string]bool, len(s.Components))
for i := range s.Components {
c := s.Components[i]
if strings.TrimSpace(c.Name) == "" {
return fmt.Errorf("%w: component[%d] name required", ErrInvalidHealth, i)

}key := strings.ToLower(strings.TrimSpace(c.Name))
if seen[key] {
return fmt.Errorf("%w: duplicate component name %q", ErrInvalidHealth, c.Name)

}seen[key] = true

if c.CheckedAt.IsZero() {
return fmt.Errorf("%w: component[%d] checked_at required", ErrInvalidHealth, i)

}if c.Status != StatusOK && c.Status != StatusDegraded && c.Status != StatusFatal && c.Status != StatusUnknown {
return fmt.Errorf("%w: component[%d] invalid status", ErrInvalidHealth, i)

}if len(c.Message) > MaxMessageLen {
return fmt.Errorf("%w: component[%d] message too long", ErrInvalidHealth, i)

}if c.Details != nil && len(c.Details) > MaxDetails {
return fmt.Errorf("%w: component[%d] too many details", ErrInvalidHealth, i)

}
}return nil
}

// StableHash returns deterministic sha256 over normalized snapshot fields.
// Warnings are intentionally excluded (hash represents health state, not normalization artifacts).
func (s HealthSnapshot) StableHash() (string, error) {
if err := s.Validate(); err != nil {
return "", err


}h := sha256.New()
write := func(x string) {
_, _ = h.Write([]byte(x))
_, _ = h.Write([]byte{0})


}write(s.Service)
write(s.Env)
write(s.Tenant)
write(s.GeneratedAt.UTC().Format(time.RFC3339Nano))
write(string(s.Overall))

for _, c := range s.Components {
write("c")
write(c.Name)
write(string(c.Status))
write(c.CheckedAt.UTC().Format(time.RFC3339Nano))
write(c.Message)

if c.Details != nil {
keys := make([]string, 0, len(c.Details))
for k := range c.Details {
keys = append(keys, k)

}sort.Strings(keys)
for _, k := range keys {
write("d:" + k)
write(c.Details[k])

}
}

}return hex.EncodeToString(h.Sum(nil)), nil
}

func normalizeStatus(s Status) Status {
switch Status(strings.ToLower(strings.TrimSpace(string(s)))) {
case StatusOK:
return StatusOK
case StatusDegraded:
return StatusDegraded
case StatusFatal:
return StatusFatal
case StatusUnknown:
return StatusUnknown
default:
return StatusUnknown

}}

// statusRank defines deterministic precedence; higher number = worse.
func statusRank(s Status) int {
switch normalizeStatus(s) {
case StatusFatal:
return 4
case StatusDegraded:
return 3
case StatusOK:
return 2
default:
return 1

}}

func hasCtl(s string) bool {
for _, r := range s {
if r < 0x20 || r == 0x7f {
return true

}
}return false
}

// ---- Deterministic JSON marshaling ----
//
// json.Marshal(map) does not guarantee key order.
// We keep the same JSON SHAPE (details as object), but marshal keys in sorted order.

func (c ComponentStatus) MarshalJSON() ([]byte, error) {
type alias struct {
Name      string            `json:"name"`
Status    Status            `json:"status"`
CheckedAt time.Time         `json:"checked_at"`
Message   string            `json:"message,omitempty"`
Details   map[string]string `json:"details,omitempty"`

}a := alias{
Name:      c.Name,
Status:    c.Status,
CheckedAt: c.CheckedAt,
Message:   c.Message,
Details:   c.Details,


}var buf bytes.Buffer
buf.WriteByte('{')

writeKV := func(key string, val []byte, comma bool) {
if comma {
buf.WriteByte(',')

}kb, _ := json.Marshal(key)
buf.Write(kb)
buf.WriteByte(':')
buf.Write(val)


}nb, _ := json.Marshal(a.Name)
writeKV("name", nb, false)

sb, _ := json.Marshal(string(a.Status))
writeKV("status", sb, true)

cb, _ := json.Marshal(a.CheckedAt)
writeKV("checked_at", cb, true)

if a.Message != "" {
mb, _ := json.Marshal(a.Message)
writeKV("message", mb, true)


}if a.Details != nil && len(a.Details) > 0 {
buf.WriteByte(',')
kb, _ := json.Marshal("details")
buf.Write(kb)
buf.WriteByte(':')
buf.WriteByte('{')

keys := make([]string, 0, len(a.Details))
for k := range a.Details {
keys = append(keys, k)

}sort.Strings(keys)
for i, k := range keys {
if i > 0 {
buf.WriteByte(',')

}kk, _ := json.Marshal(k)
vv, _ := json.Marshal(a.Details[k])
buf.Write(kk)
buf.WriteByte(':')
buf.Write(vv)

}buf.WriteByte('}')


}buf.WriteByte('}')
return buf.Bytes(), nil
}

func (s HealthSnapshot) MarshalJSON() ([]byte, error) {
type alias struct {
Service     string            `json:"service"`
Env         string            `json:"env,omitempty"`
Tenant      string            `json:"tenant,omitempty"`
GeneratedAt time.Time         `json:"generated_at"`
Overall     Status            `json:"overall"`
Components  []ComponentStatus `json:"components"`
Hash        string            `json:"hash"`
Warnings    []HealthWarning   `json:"warnings,omitempty"`

}a := alias{
Service:     s.Service,
Env:         s.Env,
Tenant:      s.Tenant,
GeneratedAt: s.GeneratedAt,
Overall:     s.Overall,
Components:  s.Components,
Hash:        s.Hash,
Warnings:    s.Warnings,

}// Struct marshaling keeps field order stable; ComponentStatus guarantees details ordering.
return json.Marshal(a)
}
