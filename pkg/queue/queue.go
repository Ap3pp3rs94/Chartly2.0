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

// Queue semantics (v0):
// - At-least-once delivery.
// - Messages are leased via visibility timeout.
// - Consumers must Ack or Nack; unacked messages become visible again after the lease.
// - Exactly-once is NOT provided; use DedupKey/EnvelopeID/idempotency at the workflow layer.
//
// This package defines CONTRACTS only (interfaces + safe envelope types).
// Backend implementations (redis, postgres, inmem) live elsewhere.
//
// Guidance:
// - Producers SHOULD set Envelope.ID for traceability and stronger idempotency.
// - Producers MAY set DedupKey for business-level deduplication (e.g., sha256(tenant|type|biz_id|day)).
// - Consumers SHOULD treat VisibilityDeadline as backend-owned (read-only).

type QueueName string
type EnvelopeID string

// Header key/value bounds (defense-in-depth).
const (
MaxHeaderPairs  = 64
MaxHeaderKeyLen = 64
MaxHeaderValLen = 256

DefaultMaxPayloadBytes = 4 * 1024 * 1024 // 4 MiB

// Opinionated guardrails to reduce drift across services.
MaxRecommendedAttempts = 10
MaxBatchSize           = 100
)

// Standard errors.
var (
ErrEmpty    = errors.New("queue: empty")
ErrClosed   = errors.New("queue: closed")
ErrOversize = errors.New("queue: oversize")
ErrInvalid  = errors.New("queue: invalid")
ErrTimeout  = errors.New("queue: timeout")
)

// Envelope is the unit of transport through the queue.
// Payload is arbitrary bytes (JSON, protobuf, etc.).
type Envelope struct {
Queue QueueName `json:"queue,omitempty"`

// ID is a stable identifier for the message.
// If not set by producer, a backend MAY set one.
ID EnvelopeID `json:"id,omitempty"`

// Type is a producer-defined classification (e.g. "job", "event", "task.connector.invoke").
Type string `json:"type"`

// Tenant is optional routing metadata; enforce at higher layers as needed.
Tenant string `json:"tenant,omitempty"`

// ProducedAt is producer-supplied timestamp (UTC recommended).
ProducedAt time.Time `json:"produced_at,omitempty"`

// Attempt is consumer/back-end managed attempt count (0 for first delivery).
Attempt int `json:"attempt,omitempty"`

// VisibilityDeadline is BACKEND-MANAGED. Backends SHOULD set this on Dequeue to indicate
// when the lease expires. Producers and consumers should treat it as read-only metadata.
VisibilityDeadline time.Time `json:"visibility_deadline,omitempty"`

// DedupKey is optional producer-supplied idempotency key. If provided, it should be stable.
DedupKey string `json:"dedup_key,omitempty"`

// Headers are low-cardinality metadata. Keys are case-insensitive in normalization.
Headers map[string]string `json:"headers,omitempty"`

// PayloadBytes is a declared size. Backends should enforce their own MaxPayloadBytes (min of limits).
PayloadBytes int64 `json:"payload_bytes,omitempty"`

// Payload is the message body.
Payload []byte `json:"payload,omitempty"`
}

// Validate is a convenience method that enforces normalization + bounds.
func (env Envelope) Validate() error {
_, err := NormalizeEnvelope(env)
return err
}

// DequeueResult is returned by Dequeue. Receipt is an opaque token needed for Ack/Nack/Extend.
type DequeueResult struct {
Env     Envelope `json:"env"`
Receipt string   `json:"receipt"` // opaque token issued by backend
}

// Producer publishes messages.
type Producer interface {
Enqueue(ctx context.Context, q QueueName, env Envelope) error

// EnqueueBatch publishes many envelopes. Implementations SHOULD enforce MaxBatchSize.
EnqueueBatch(ctx context.Context, q QueueName, envs []Envelope) error
}

// Consumer leases messages for processing.
type Consumer interface {
// Dequeue returns ErrEmpty if no message is available within the poll interval.
// Implementations may block up to pollTimeout (or return immediately).
Dequeue(ctx context.Context, q QueueName, pollTimeout time.Duration, visibilityTimeout time.Duration) (DequeueResult, error)

// Ack permanently removes a leased message.
Ack(ctx context.Context, q QueueName, receipt string) error

// Nack returns a leased message to the queue; delay controls re-visibility.
Nack(ctx context.Context, q QueueName, receipt string, delay time.Duration) error

// NackWithDeadLetter returns the message to the queue OR moves it to DLQ depending on backend policy.
// This is the preferred poison-pill safe API: callers provide a reason, backends can decide:
// - if attempt >= max => DLQ
// - else requeue with delay
// Backends that do not support DLQ SHOULD return ErrInvalid.
NackWithDeadLetter(ctx context.Context, q QueueName, receipt string, delay time.Duration, reason string) error

// ExtendVisibility extends the lease on a leased message.
ExtendVisibility(ctx context.Context, q QueueName, receipt string, visibilityTimeout time.Duration) error
}

// DeadLetter supports explicit poison-message routing.
// Still useful for administrative flows or explicit moves.
type DeadLetter interface {
MoveToDLQ(ctx context.Context, q QueueName, receipt string, reason string) error
}

// Queue combines producer+consumer.
type Queue interface {
Producer
Consumer
}

// NormalizeEnvelope applies deterministic normalization and validates bounds.
// It does NOT mutate payload bytes content (but it may trim strings and normalize header keys).
func NormalizeEnvelope(env Envelope) (Envelope, error) {
env.Type = strings.TrimSpace(env.Type)
env.Tenant = strings.TrimSpace(env.Tenant)
env.DedupKey = strings.TrimSpace(env.DedupKey)

if env.Attempt < 0 {
return Envelope{}, fmt.Errorf("%w: attempt cannot be negative", ErrInvalid)

}if env.PayloadBytes < 0 {
return Envelope{}, fmt.Errorf("%w: payload_bytes cannot be negative", ErrInvalid)

}if env.PayloadBytes == 0 && len(env.Payload) > 0 {
env.PayloadBytes = int64(len(env.Payload))


}// Normalize headers deterministically (lowercase keys, trimmed values, bounded).
if env.Headers != nil {
clean := make(map[string]string, len(env.Headers))
keys := make([]string, 0, len(env.Headers))
for k := range env.Headers {
keys = append(keys, k)

}sort.Strings(keys)
for _, k := range keys {
k2 := strings.ToLower(strings.TrimSpace(k))
if k2 == "" || len(k2) > MaxHeaderKeyLen {
continue

}v := strings.TrimSpace(env.Headers[k])
if len(v) > MaxHeaderValLen {
v = v[:MaxHeaderValLen]

}clean[k2] = v
if len(clean) >= MaxHeaderPairs {
break

}
}if len(clean) == 0 {
env.Headers = nil
} else {
env.Headers = clean

}

}if env.Type == "" {
return Envelope{}, fmt.Errorf("%w: type is required", ErrInvalid)

}if len(env.Type) > 128 {
return Envelope{}, fmt.Errorf("%w: type too long", ErrInvalid)

}if env.DedupKey != "" && len(env.DedupKey) > 256 {
return Envelope{}, fmt.Errorf("%w: dedup_key too long", ErrInvalid)

}if env.PayloadBytes > int64(DefaultMaxPayloadBytes) {
return Envelope{}, fmt.Errorf("%w: payload_bytes exceeds default max (%d)", ErrOversize, DefaultMaxPayloadBytes)

}if len(env.Payload) > 0 && int64(len(env.Payload)) != env.PayloadBytes {
// Producer declared size mismatch
return Envelope{}, fmt.Errorf("%w: payload_bytes mismatch (declared=%d actual=%d)", ErrInvalid, env.PayloadBytes, len(env.Payload))


}return env, nil
}

// StableEnvelopeHash returns a deterministic sha256 over envelope metadata + payload bytes.
// Useful for idempotency keys and audit trails.
func StableEnvelopeHash(env Envelope) (string, error) {
n, err := NormalizeEnvelope(env)
if err != nil {
return "", err

}h := sha256.New()

write := func(s string) {
_, _ = h.Write([]byte(s))
_, _ = h.Write([]byte{0})


}write(string(n.Queue))
write(string(n.ID))
write(n.Type)
write(n.Tenant)
write(n.DedupKey)
write(fmt.Sprintf("%d", n.Attempt))
write(fmt.Sprintf("%d", n.PayloadBytes))

if n.Headers != nil {
keys := make([]string, 0, len(n.Headers))
for k := range n.Headers {
keys = append(keys, k)

}sort.Strings(keys)
for _, k := range keys {
write("h:" + k)
write(n.Headers[k])

}

}if len(n.Payload) > 0 {
_, _ = h.Write(n.Payload)


}return hex.EncodeToString(h.Sum(nil)), nil
}
