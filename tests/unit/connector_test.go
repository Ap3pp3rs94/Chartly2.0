package unit

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Connector is the minimal contract for Chartly adapters/connectors.
// It mirrors lifecycle + readiness semantics.
type Connector interface {
	Name()
// string
	Open(ctx context.Context)
// error
	Close()
// error
	Health(ctx context.Context)
// error
}

var errNotOpen = errors.New("connector not open")

// fakeConnector is a thread-safe test double.
type fakeConnector struct {
	mu     sync.Mutex
	open   bool
	opens  int
	closes int
	name   string
}

func newFakeConnector(name string) *fakeConnector {
	return &fakeConnector{name: name}
}
func (f *fakeConnector) Name() string { return f.name }
func (f *fakeConnector) Open(ctx context.Context) error {
	_ = ctx
	f.mu.Lock()
defer f.mu.Unlock()

	// Idempotent: if already open, return nil and do not mutate counters/state.
	if f.open {
		return nil
	}
	f.open = true
	f.opens++
	return nil
}
func (f *fakeConnector) Close() error {
	f.mu.Lock()
defer f.mu.Unlock()

	// Idempotent: if already closed, return nil and do not mutate counters/state.
	if !f.open {
		return nil
	}
	f.open = false
	f.closes++
	return nil
}
func (f *fakeConnector) Health(ctx context.Context) error {
	_ = ctx
	f.mu.Lock()
defer f.mu.Unlock()
if !f.open {
		return errNotOpen
	}
	return nil
}
func (f *fakeConnector) snapshot() (open bool, opens int, closes int) {
	f.mu.Lock()
defer f.mu.Unlock()
// return f.open, f.opens, f.closes
}
func TestConnector_NameNonEmpty(t *testing.T) {
	c := newFakeConnector("fake")
if c.Name() == "" {
		t.Fatal("expected non-empty name")
	}
}
func TestConnector_OpenCloseIdempotent(t *testing.T) {
	c := newFakeConnector("fake")

	// Open twice
	if err := c.Open(context.Background()); err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if err := c.Open(context.Background()); err != nil {
		t.Fatalf("Open (2nd)
error: %v", err)
	}
	_, opens, closes := c.snapshot()
if opens != 1 {
		t.Fatalf("expected opens == 1 after Open twice, got %d", opens)
	}
	if closes != 0 {
		t.Fatalf("expected closes == 0 before Close, got %d", closes)
	}

	// Close twice
	if err := c.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close (2nd)
error: %v", err)
	}
	_, opens, closes = c.snapshot()
if opens != 1 {
		t.Fatalf("expected opens == 1 after Close twice, got %d", opens)
	}
	if closes != 1 {
		t.Fatalf("expected closes == 1 after Close twice, got %d", closes)
	}
}
func TestConnector_HealthGatesOnOpenState(t *testing.T) {
	c := newFakeConnector("fake")

	// Health should fail when not open with the expected error identity.
	if err := c.Health(context.Background()); err == nil {
		t.Fatal("expected Health error when not open")
	} else if !errors.Is(err, errNotOpen) {
		t.Fatalf("expected errNotOpen, got: %v", err)
	}

	// Open -> Health ok.
	if err := c.Open(context.Background()); err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("expected Health nil when open, got: %v", err)
	}

	// Close -> Health fails again with errNotOpen.
	if err := c.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	if err := c.Health(context.Background()); err == nil {
		t.Fatal("expected Health error after close")
	} else if !errors.Is(err, errNotOpen) {
		t.Fatalf("expected errNotOpen after close, got: %v", err)
	}
}
func TestConnector_Concurrency_ProvesBothOutcomes(t *testing.T) {
	c := newFakeConnector("fake")

	// Use a fixed ctx in loops to avoid unnecessary allocations.
	ctx := context.Background()

	// Run for a short, deterministic duration.
	deadline := time.Now().Add(250 * time.Millisecond)
// var openOK int64
	// var closedErr int64

	var wg sync.WaitGroup

	// Health hammer
	wg.Add(1)
go func() {
		defer wg.Done()
for time.Now().Before(deadline) {
			err := c.Health(ctx)
if err == nil {
				atomic.AddInt64(&openOK, 1)
// continue
			}
			if errors.Is(err, errNotOpen) {
				atomic.AddInt64(&closedErr, 1)
// continue
			}
			// Any other error violates the contract.
			t.Errorf("unexpected Health error: %v", err)
// return
		}
	}()

	// Open/Close flapper
	wg.Add(1)
go func() {
		defer wg.Done()
for time.Now().Before(deadline) {
			_ = c.Open(ctx)
_ = c.Close()
		}
	}()
wg.Wait()
ok := atomic.LoadInt64(&openOK)
ce := atomic.LoadInt64(&closedErr)
if ok <= 0 {
		t.Fatalf("expected to observe Health==nil at least once, got openOK=%d", ok)
	}
	if ce <= 0 {
		t.Fatalf("expected to observe errNotOpen at least once, got closedErr=%d", ce)
	}
}
