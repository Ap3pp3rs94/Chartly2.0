package idempotency

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// type State string

const (
	StateInProgress State = "in_progress"
	StateComplete   State = "complete"
	StateFailed     State = "failed"
)

var (
	ErrInvalid  = errors.New("idempotency: invalid")
	ErrConflict = errors.New("idempotency: conflict")
	ErrNotOwner = errors.New("idempotency: not owner")
	ErrExpired  = errors.New("idempotency: expired")
	ErrTooLarge = errors.New("idempotency: too large")
)

type Clock interface {
	Now()
	// time.Time
}
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

type Options struct {
	// Hard caps
	// MaxEntries     int   // default 200000
	// MaxResultBytes int64 // default 1 MiB
	// Default TTL used when caller passes ttl<=0
	// DefaultTTL time.Duration // default 5m

	// Deterministic testing
	// Clock Clock

	// Opportunistic pruning: every N writes we prune expired entries.
	// PruneEvery int // default 1024
}

// Record is the canonical idempotency entry.
// ResultBytes is optional; some services may store a pointer (URI)
// elsewhere and only store hashes here later.
type Record struct {
	Key string `json:"key"`

	State State `json:"state"`

	// OwnerToken is set when in_progress and must match to Complete/Fail/Touch.
	OwnerToken string `json:"owner_token,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	ExpiresAt time.Time `json:"expires_at"`

	// Result (for complete)
	ResultHash  string `json:"result_hash,omitempty"` // sha256 hex of ResultBytes
	ResultBytes []byte `json:"result_bytes,omitempty"`

	// Failure (for failed)
	ErrorCode string `json:"error_code,omitempty"`
	ErrorMsg  string `json:"error_msg,omitempty"`
}

func (r Record) IsExpired(now time.Time) bool {
	if r.ExpiresAt.IsZero() {
		return false

	}
	return now.UTC().After(r.ExpiresAt.UTC())
}

type BeginResult struct {
	// Record is the current record after TryBegin (either existing or newly created).
	Record Record `json:"record"`
	// Fresh indicates whether the caller acquired a new lease (should do work).
	Fresh bool `json:"fresh"`
}

// Store defines the idempotency persistence contract.
type Store interface {
	// TryBegin attempts to acquire a lease for key. If key is complete, returns Fresh=false with the record.
	// If key is in_progress and not expired, returns Fresh=false with ErrConflict.
	// If key is expired, it is replaced and Fresh=true.
	TryBegin(ctx context.Context, key string, ttl time.Duration) (BeginResult, error)

	// Touch extends the lease if caller holds ownership (in_progress only).
	Touch(ctx context.Context, key string, ownerToken string, ttl time.Duration) (Record, error)

	// Complete marks the key complete (requires owner token).
	Complete(ctx context.Context, key string, ownerToken string, result []byte) (Record, error)

	// Fail marks the key failed (requires owner token).
	Fail(ctx context.Context, key string, ownerToken string, code string, msg string) (Record, error)

	// Get fetches record.
	Get(ctx context.Context, key string) (Record, bool, error)

	// Delete removes record (admin/debug).
	Delete(ctx context.Context, key string)
	// error

	// Sweep removes expired entries. Deterministic, no background goroutine.
	Sweep(ctx context.Context) (removed int, err error)
}

// MemoryStore is a reference in-memory implementation (suitable for local/dev).
// It is not durable.
type MemoryStore struct {
	mu    sync.Mutex
	opts  Options
	clock Clock

	m map[string]Record

	ops uint64
}

// NewMemoryStore constructs an in-memory store with defaults applied.
func NewMemoryStore(opts Options) *MemoryStore {
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = 200000

	}
	if opts.MaxResultBytes <= 0 {
		opts.MaxResultBytes = 1 * 1024 * 1024

	}
	if opts.DefaultTTL <= 0 {
		opts.DefaultTTL = 5 * time.Minute

	}
	if opts.Clock == nil {
		opts.Clock = systemClock{}

	}
	if opts.PruneEvery <= 0 {
		opts.PruneEvery = 1024

	}
	return &MemoryStore{
		opts:  opts,
		clock: opts.Clock,
		m:     make(map[string]Record, 1024),
	}
}
func (s *MemoryStore) TryBegin(ctx context.Context, key string, ttl time.Duration) (BeginResult, error) {
	if ctx == nil {
		ctx = context.Background()

	}
	if err := ctx.Err(); err != nil {
		return BeginResult{}, err

	}
	if err := ValidateKey(key); err != nil {
		return BeginResult{}, ErrInvalid

	}
	if ttl <= 0 {
		ttl = s.opts.DefaultTTL

	}
	now := s.clock.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ops++
	if s.ops%uint64(s.opts.PruneEvery) == 0 {
		s.pruneLocked(now)

	}
	if rec, ok := s.m[key]; ok {
		if rec.IsExpired(now) {
			// replace expired
			nr := s.newInProgressLocked(key, now, ttl)
			s.m[key] = nr
			return BeginResult{Record: nr, Fresh: true}, nil

		}
		switch rec.State {
		case StateComplete, StateFailed:
			return BeginResult{Record: rec, Fresh: false}, nil
		case StateInProgress:
			return BeginResult{Record: rec, Fresh: false}, ErrConflict
		default:
			// unknown state: treat as replace
			nr := s.newInProgressLocked(key, now, ttl)
			s.m[key] = nr
			return BeginResult{Record: nr, Fresh: true}, nil

		}

	} // new record
	if len(s.m) >= s.opts.MaxEntries {
		// prune first; if still full, reject
		s.pruneLocked(now)
		if len(s.m) >= s.opts.MaxEntries {
			return BeginResult{}, ErrTooLarge

		}

	}
	nr := s.newInProgressLocked(key, now, ttl)
	s.m[key] = nr
	return BeginResult{Record: nr, Fresh: true}, nil
}
func (s *MemoryStore) Touch(ctx context.Context, key string, ownerToken string, ttl time.Duration) (Record, error) {
	if ctx == nil {
		ctx = context.Background()

	}
	if err := ctx.Err(); err != nil {
		return Record{}, err

	}
	if err := ValidateKey(key); err != nil {
		return Record{}, ErrInvalid

	}
	ownerToken = stringsTrim(ownerToken)
	if ownerToken == "" {
		return Record{}, ErrInvalid

	}
	if ttl <= 0 {
		ttl = s.opts.DefaultTTL

	}
	now := s.clock.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.m[key]
	if !ok {
		return Record{}, ErrExpired

	}
	if rec.IsExpired(now) {
		delete(s.m, key)
		return Record{}, ErrExpired

	}
	if rec.State != StateInProgress {
		return rec, ErrConflict

	}
	if rec.OwnerToken != ownerToken {
		return rec, ErrNotOwner

	}
	rec.UpdatedAt = now
	rec.ExpiresAt = now.Add(ttl)
	s.m[key] = rec
	return rec, nil
}
func (s *MemoryStore) Complete(ctx context.Context, key string, ownerToken string, result []byte) (Record, error) {
	if ctx == nil {
		ctx = context.Background()

	}
	if err := ctx.Err(); err != nil {
		return Record{}, err

	}
	if err := ValidateKey(key); err != nil {
		return Record{}, ErrInvalid

	}
	ownerToken = stringsTrim(ownerToken)
	if ownerToken == "" {
		return Record{}, ErrInvalid

	}
	if int64(len(result)) > s.opts.MaxResultBytes {
		return Record{}, ErrTooLarge

	}
	now := s.clock.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.m[key]
	if !ok || rec.IsExpired(now) {
		if ok {
			delete(s.m, key)

		}
		return Record{}, ErrExpired

	}
	if rec.State != StateInProgress {
		return rec, ErrConflict

	}
	if rec.OwnerToken != ownerToken {
		return rec, ErrNotOwner

	}
	sum := sha256.Sum256(result)
	rec.State = StateComplete
	rec.OwnerToken = "" // release ownership
	rec.UpdatedAt = now
	rec.ResultBytes = append([]byte(nil), result...)
	rec.ResultHash = hex.EncodeToString(sum[:])
	// keep ExpiresAt as-is (allows client to fetch completed result for TTL window)
	s.m[key] = rec
	return rec, nil
}
func (s *MemoryStore) Fail(ctx context.Context, key string, ownerToken string, code string, msg string) (Record, error) {
	if ctx == nil {
		ctx = context.Background()

	}
	if err := ctx.Err(); err != nil {
		return Record{}, err

	}
	if err := ValidateKey(key); err != nil {
		return Record{}, ErrInvalid

	}
	ownerToken = stringsTrim(ownerToken)
	if ownerToken == "" {
		return Record{}, ErrInvalid

	}
	code = stringsTrim(code)
	msg = stringsTrim(msg)
	if len(code) > 64 {
		code = code[:64]

	}
	if len(msg) > 512 {
		msg = msg[:512]

	}
	now := s.clock.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.m[key]
	if !ok || rec.IsExpired(now) {
		if ok {
			delete(s.m, key)

		}
		return Record{}, ErrExpired

	}
	if rec.State != StateInProgress {
		return rec, ErrConflict

	}
	if rec.OwnerToken != ownerToken {
		return rec, ErrNotOwner

	}
	rec.State = StateFailed
	rec.OwnerToken = ""
	rec.UpdatedAt = now
	rec.ErrorCode = code
	rec.ErrorMsg = msg
	rec.ResultBytes = nil
	rec.ResultHash = ""

	s.m[key] = rec
	return rec, nil
}
func (s *MemoryStore) Get(ctx context.Context, key string) (Record, bool, error) {
	if ctx == nil {
		ctx = context.Background()

	}
	if err := ctx.Err(); err != nil {
		return Record{}, false, err

	}
	if err := ValidateKey(key); err != nil {
		return Record{}, false, ErrInvalid

	}
	now := s.clock.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.m[key]
	if !ok {
		return Record{}, false, nil

	}
	if rec.IsExpired(now) {
		delete(s.m, key)
		return Record{}, false, nil

	}
	return rec, true, nil
}
func (s *MemoryStore) Delete(ctx context.Context, key string) error {
	if ctx == nil {
		ctx = context.Background()

	}
	if err := ctx.Err(); err != nil {
		return err

	}
	if err := ValidateKey(key); err != nil {
		return ErrInvalid

	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}
func (s *MemoryStore) Sweep(ctx context.Context) (int, error) {
	if ctx == nil {
		ctx = context.Background()

	}
	if err := ctx.Err(); err != nil {
		return 0, err

	}
	now := s.clock.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pruneLocked(now), nil
}
func (s *MemoryStore) pruneLocked(now time.Time) int {
	removed := 0
	for k, rec := range s.m {
		if rec.IsExpired(now) {
			delete(s.m, k)
			removed++

		}
	}
	return removed
}
func (s *MemoryStore) newInProgressLocked(key string, now time.Time, ttl time.Duration) Record {
	return Record{
		Key:        key,
		State:      StateInProgress,
		OwnerToken: newOwnerToken(),
		CreatedAt:  now,
		UpdatedAt:  now,
		ExpiresAt:  now.Add(ttl),
	}
}

// newOwnerToken generates an opaque token for ownership checks.
func newOwnerToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
func stringsTrim(s string) string {
	return strings.TrimSpace(s)
}
