package queue

import (
"context"
"crypto/sha256"
"encoding/hex"
"errors"
"fmt"
"sort"
"strings"
"time"
)

const (
MaxReasonLen    = 512
MaxExtraFields  = 32
MaxExtraKeyLen  = 64
MaxExtraValLen  = 256
MaxRecordIDLen  = 128
MaxQueueNameLen = 128
)

// DLQRecord captures why an Envelope was dead-lettered.
// This is a pure data contract: storage backends implement DLQStore.
//
// Timestamp semantics (intended, to prevent drift):
// - FirstSeenAt: set by the consumer/runner when the message is first observed failing
//   (e.g., first dequeue that led to a retry or DLQ decision).
// - LastSeenAt: updated by the consumer/runner on subsequent failures/retries and/or on DLQ move.
// - DeadLetteredAt: when the record is written to DLQ (required).
//
// Backends may persist these fields, but should not invent their meaning.
type DLQRecord struct {
// RecordID is a stable identifier for this DLQ entry (backend may generate).
RecordID string `json:"record_id,omitempty"`

Queue QueueName `json:"queue"`

// Original envelope metadata/body. Payload may be present (bounded by queue contract) but backends may store separately.
Envelope Envelope `json:"envelope"`

// FinalAttempt is the attempt number when it was dead-lettered.
FinalAttempt int `json:"final_attempt"`

// Reason is a human/programmable reason code or message.
Reason string `json:"reason"`

FirstSeenAt time.Time `json:"first_seen_at,omitempty"`
LastSeenAt  time.Time `json:"last_seen_at,omitempty"`

// DeadLetteredAt is when it entered DLQ (required).
DeadLetteredAt time.Time `json:"dead_lettered_at"`

// Extra is small, low-cardinality metadata for investigations (bounded).
Extra map[string]string `json:"extra,omitempty"`

// RecordHash is optional; can be computed via StableHash() over the normalized record.
RecordHash string `json:"record_hash,omitempty"`
}

var (
ErrDLQInvalid = errors.New("queue: dlq invalid")
)

// DLQStore is an abstract DLQ persistence interface.
// Implementations may store payload separately; record may omit large payloads.
type DLQStore interface {
Put(ctx context.Context, rec DLQRecord) error
Get(ctx context.Context, recordID string) (DLQRecord, error)
List(ctx context.Context, q QueueName, limit int) ([]DLQRecord, error)
Delete(ctx context.Context, recordID string) error
}

// NewDLQRecord constructs a DLQRecord with optional time override.
// If now is zero, uses time.Now().UTC().
// The returned record is normalized + validated.
func NewDLQRecord(q QueueName, env Envelope, finalAttempt int, reason string, now time.Time) (DLQRecord, error) {
if now.IsZero() {
now = time.Now().UTC()
} else {
now = now.UTC()


}normEnv, err := NormalizeEnvelope(env)
if err != nil {
return DLQRecord{}, err


}rec := DLQRecord{
Queue:          q,
Envelope:       normEnv,
FinalAttempt:   finalAttempt,
Reason:         reason,
DeadLetteredAt: now,

}return NormalizeDLQRecord(rec)
}

// NormalizeDLQRecord returns a normalized copy of the record and validates it.
// Normalization rules:
// - trims Reason; truncates to MaxReasonLen
// - lowercases/trim Extra keys; trims values; truncates to MaxExtraValLen; enforces MaxExtraFields
// - ensures timestamps are UTC
// - lowercases RecordHash if set
// - normalizes Envelope via NormalizeEnvelope
func NormalizeDLQRecord(r DLQRecord) (DLQRecord, error) {
out := r

out.RecordID = strings.TrimSpace(out.RecordID)
if len(out.RecordID) > MaxRecordIDLen {
out.RecordID = out.RecordID[:MaxRecordIDLen]


}// Queue: trimmed semantic (QueueName is string alias)
out.Queue = QueueName(strings.TrimSpace(string(out.Queue)))

// Normalize envelope using the shared contract.
env, err := NormalizeEnvelope(out.Envelope)
if err != nil {
return DLQRecord{}, err

}out.Envelope = env

// Attempts
if out.FinalAttempt < 0 {
out.FinalAttempt = 0


}// Reason
out.Reason = strings.TrimSpace(out.Reason)
if len(out.Reason) > MaxReasonLen {
out.Reason = out.Reason[:MaxReasonLen]


}// Times: UTC, zero allowed for First/LastSeen
if !out.FirstSeenAt.IsZero() {
out.FirstSeenAt = out.FirstSeenAt.UTC()

}if !out.LastSeenAt.IsZero() {
out.LastSeenAt = out.LastSeenAt.UTC()

}if !out.DeadLetteredAt.IsZero() {
out.DeadLetteredAt = out.DeadLetteredAt.UTC()


}// Extra
if out.Extra != nil {
clean := make(map[string]string, len(out.Extra))
keys := make([]string, 0, len(out.Extra))
for k := range out.Extra {
keys = append(keys, k)

}sort.Strings(keys) // deterministic selection when truncating
for _, k := range keys {
k2 := strings.ToLower(strings.TrimSpace(k))
if k2 == "" || len(k2) > MaxExtraKeyLen {
continue

}v := strings.TrimSpace(out.Extra[k])
if len(v) > MaxExtraValLen {
v = v[:MaxExtraValLen]

}clean[k2] = v
if len(clean) >= MaxExtraFields {
break

}
}if len(clean) == 0 {
out.Extra = nil
} else {
out.Extra = clean

}

}out.RecordHash = strings.TrimSpace(strings.ToLower(out.RecordHash))

if err := out.Validate(); err != nil {
return DLQRecord{}, err

}return out, nil
}

func (r DLQRecord) Validate() error {
if strings.TrimSpace(string(r.Queue)) == "" {
return fmt.Errorf("%w: queue required", ErrDLQInvalid)

}if len(string(r.Queue)) > MaxQueueNameLen {
return fmt.Errorf("%w: queue too long", ErrDLQInvalid)

}if r.RecordID != "" && len(r.RecordID) > MaxRecordIDLen {
return fmt.Errorf("%w: record_id too long", ErrDLQInvalid)

}if r.FinalAttempt < 0 {
return fmt.Errorf("%w: final_attempt cannot be negative", ErrDLQInvalid)

}if r.DeadLetteredAt.IsZero() {
return fmt.Errorf("%w: dead_lettered_at required", ErrDLQInvalid)

}if len(r.Reason) > MaxReasonLen {
return fmt.Errorf("%w: reason too long", ErrDLQInvalid)


}// Envelope contract
if err := r.Envelope.Validate(); err != nil {
return err


}// Extra bounds
if r.Extra != nil {
if len(r.Extra) > MaxExtraFields {
return fmt.Errorf("%w: too many extra fields", ErrDLQInvalid)

}for k, v := range r.Extra {
k2 := strings.ToLower(strings.TrimSpace(k))
if k2 == "" || len(k2) > MaxExtraKeyLen {
return fmt.Errorf("%w: invalid extra key", ErrDLQInvalid)

}v2 := strings.TrimSpace(v)
if len(v2) > MaxExtraValLen {
return fmt.Errorf("%w: extra value too long", ErrDLQInvalid)

}
}

}// RecordHash if present must look like lowercase sha256 hex.
if r.RecordHash != "" {
if len(r.RecordHash) != 64 || !isHexLower(r.RecordHash) {
return fmt.Errorf("%w: invalid record_hash", ErrDLQInvalid)

}

}return nil
}

// StableHash computes a deterministic sha256 over the *normalized* DLQ record + envelope hash.
// It does NOT include RecordID (backend may assign) and excludes RecordHash itself.
func (r DLQRecord) StableHash() (string, error) {
// Normalize first to guarantee symmetry.
tmp, err := NormalizeDLQRecord(r)
if err != nil {
return "", err

}tmp.RecordHash = "" // excluded
tmp.RecordID = ""   // excluded from stable hash

h := sha256.New()
write := func(s string) {
_, _ = h.Write([]byte(s))
_, _ = h.Write([]byte{0})


}write(string(tmp.Queue))
write(string(tmp.Envelope.ID))
write(tmp.Envelope.Type)
write(tmp.Envelope.Tenant)
write(tmp.Envelope.DedupKey)
write(fmt.Sprintf("%d", tmp.FinalAttempt))
write(tmp.Reason)
write(tmp.DeadLetteredAt.UTC().Format(time.RFC3339Nano))

if !tmp.FirstSeenAt.IsZero() {
write("first_seen_at")
write(tmp.FirstSeenAt.UTC().Format(time.RFC3339Nano))

}if !tmp.LastSeenAt.IsZero() {
write("last_seen_at")
write(tmp.LastSeenAt.UTC().Format(time.RFC3339Nano))


}if tmp.Extra != nil {
keys := make([]string, 0, len(tmp.Extra))
for k := range tmp.Extra {
keys = append(keys, k)

}sort.Strings(keys)
for _, k := range keys {
write("x:" + k)
write(tmp.Extra[k])

}

}// Include stable envelope hash (includes payload)
eh, err := StableEnvelopeHash(tmp.Envelope)
if err != nil {
return "", err

}write("envhash")
write(eh)

return hex.EncodeToString(h.Sum(nil)), nil
}

func isHexLower(s string) bool {
for _, r := range s {
if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
continue

}return false

}return true
}
