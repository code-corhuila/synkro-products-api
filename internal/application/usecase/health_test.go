package usecase

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/in"
	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
)

var (
	_ in.HealthUseCases = (*HealthService)(nil)
	_ out.StoreHealth   = (*fakeStoreHealth)(nil)
)

const (
	healthTestTimeout = 50 * time.Millisecond
	// healthTestBound is far above healthTestTimeout: it only proves a check
	// returns instead of hanging, it is not a performance assertion.
	healthTestBound = 5 * time.Second
)

// fakeStoreHealth honors its context like a real port must. It records what
// the use case handed it.
type fakeStoreHealth struct {
	mu          sync.Mutex
	calls       int
	hasDeadline bool
	deadline    time.Time
	ctxErr      error
	started     chan struct{}
	once        sync.Once
	behave      func(ctx context.Context) error
}

func (f *fakeStoreHealth) Check(ctx context.Context) error {
	f.mu.Lock()
	f.calls++
	f.deadline, f.hasDeadline = ctx.Deadline()
	f.mu.Unlock()
	if f.started != nil {
		f.once.Do(func() { close(f.started) })
	}
	var err error
	if f.behave != nil {
		err = f.behave(ctx)
	}
	f.mu.Lock()
	f.ctxErr = ctx.Err()
	f.mu.Unlock()
	return err
}

func (f *fakeStoreHealth) snapshot() (calls int, hasDeadline bool, deadline time.Time, ctxErr error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.hasDeadline, f.deadline, f.ctxErr
}

func waitForContext(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

type healthLogs struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *healthLogs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *healthLogs) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// captureHealthLogs routes slog (and therefore the log package) to a buffer
// for the test. Tests using it must not run in parallel.
func captureHealthLogs(t *testing.T) *healthLogs {
	t.Helper()
	logs := &healthLogs{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return logs
}

func TestHealthService_AvailableStoreIsHealthy(t *testing.T) {
	store := &fakeStoreHealth{}
	svc := NewHealthService(store, healthTestTimeout)

	if !svc.Healthy(context.Background()) {
		t.Fatal("expected healthy when the store check succeeds")
	}
	if calls, _, _, _ := store.snapshot(); calls != 1 {
		t.Fatalf("expected exactly one store check, got %d", calls)
	}
}

func TestHealthService_StoreErrorIsUnhealthy(t *testing.T) {
	store := &fakeStoreHealth{behave: func(context.Context) error { return errors.New("store unavailable") }}
	svc := NewHealthService(store, healthTestTimeout)

	if svc.Healthy(context.Background()) {
		t.Fatal("expected unhealthy when the store check fails")
	}
	if calls, _, _, _ := store.snapshot(); calls != 1 {
		t.Fatalf("expected exactly one store check, got %d", calls)
	}
}

func TestHealthService_EachCheckGetsItsOwnBoundedDeadline(t *testing.T) {
	store := &fakeStoreHealth{}
	svc := NewHealthService(store, healthTestTimeout)

	before := time.Now()
	svc.Healthy(context.Background())
	after := time.Now()

	_, hasDeadline, first, _ := store.snapshot()
	if !hasDeadline {
		t.Fatal("the store must receive a context with a deadline, the parent has none")
	}
	if first.Before(before.Add(healthTestTimeout)) || first.After(after.Add(healthTestTimeout)) {
		t.Fatalf("deadline %v is not now+%v (between %v and %v)", first, healthTestTimeout, before, after)
	}

	svc.Healthy(context.Background())
	calls, _, second, _ := store.snapshot()
	if calls != 2 {
		t.Fatalf("expected one store check per call, got %d", calls)
	}
	if !second.After(first) {
		t.Fatalf("second check reused the first deadline: %v then %v", first, second)
	}
}

func TestHealthService_ParentDeadlineShorterThanTheTimeoutIsPreserved(t *testing.T) {
	store := &fakeStoreHealth{}
	svc := NewHealthService(store, time.Hour)

	parentDeadline := time.Now().Add(5 * time.Second)
	parent, cancel := context.WithDeadline(context.Background(), parentDeadline)
	defer cancel()

	if !svc.Healthy(parent) {
		t.Fatal("expected healthy")
	}
	_, hasDeadline, got, _ := store.snapshot()
	if !hasDeadline || got.After(parentDeadline) {
		t.Fatalf("the check must not outlive the parent: parent %v, check %v (has deadline: %v)", parentDeadline, got, hasDeadline)
	}
}

func TestHealthService_SlowStoreIsUnhealthyWhenTheTimeoutExpires(t *testing.T) {
	store := &fakeStoreHealth{behave: waitForContext}
	svc := NewHealthService(store, healthTestTimeout)

	start := time.Now()
	healthy := svc.Healthy(context.Background())
	elapsed := time.Since(start)

	if healthy {
		t.Fatal("a store that never answers must be unhealthy")
	}
	if elapsed > healthTestBound {
		t.Fatalf("the check took %v, it must be bounded by the timeout", elapsed)
	}
	if _, _, _, ctxErr := store.snapshot(); !errors.Is(ctxErr, context.DeadlineExceeded) {
		t.Fatalf("the store should have been stopped by the deadline, context error: %v", ctxErr)
	}
}

func TestHealthService_ParentCancelledWhileCheckingIsUnhealthy(t *testing.T) {
	store := &fakeStoreHealth{behave: waitForContext, started: make(chan struct{})}
	// The service timeout is huge on purpose: only the parent can stop it.
	svc := NewHealthService(store, time.Hour)

	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-store.started
		cancel()
	}()

	start := time.Now()
	healthy := svc.Healthy(parent)
	elapsed := time.Since(start)

	if healthy {
		t.Fatal("a cancelled parent must make the check unhealthy")
	}
	if elapsed > healthTestBound {
		t.Fatalf("cancellation took %v to be honored", elapsed)
	}
	if _, _, _, ctxErr := store.snapshot(); !errors.Is(ctxErr, context.Canceled) {
		t.Fatalf("the store should see the parent's cancellation, context error: %v", ctxErr)
	}
}

func TestHealthService_AlreadyCancelledParentIsUnhealthy(t *testing.T) {
	cases := []struct {
		name   string
		behave func(ctx context.Context) error
	}{
		{"store honors its context", waitForContext},
		{"store answers anyway", func(context.Context) error { return nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStoreHealth{behave: tc.behave}
			svc := NewHealthService(store, time.Hour)

			parent, cancel := context.WithCancel(context.Background())
			cancel()

			if svc.Healthy(parent) {
				t.Fatal("a probe whose caller already gave up must not report healthy")
			}
		})
	}
}

func TestHealthService_StoreErrorDetailsNeverReachTheLogs(t *testing.T) {
	const sentinel = "postgres://svc_user:SENTINEL-PASSWORD@db.internal:5432/products"
	logs := captureHealthLogs(t)
	store := &fakeStoreHealth{behave: func(context.Context) error {
		return errors.New("dial " + sentinel + ": connection refused")
	}}
	svc := NewHealthService(store, healthTestTimeout)

	// Healthy returns only a bool: there is nothing else to leak through its
	// result, so the logs are the remaining channel.
	var _ func(context.Context) bool = svc.Healthy
	if svc.Healthy(context.Background()) {
		t.Fatal("expected unhealthy")
	}
	for _, secret := range []string{"SENTINEL-PASSWORD", "svc_user", "db.internal", "connection refused"} {
		if bytes.Contains([]byte(logs.String()), []byte(secret)) {
			t.Fatalf("log output leaks %q: %s", secret, logs.String())
		}
	}
}

// A non-positive timeout is a wiring bug. The constructor has no error
// result, so either refusing at construction (panic) or failing closed at
// check time without ever calling the store is acceptable; reporting healthy,
// or calling the store with an unbounded context, is not.
func TestHealthService_NonPositiveTimeoutIsRejected(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Nanosecond, -time.Second} {
		t.Run(timeout.String(), func(t *testing.T) {
			store := &fakeStoreHealth{}
			var svc *HealthService
			func() {
				defer func() { _ = recover() }()
				svc = NewHealthService(store, timeout)
			}()
			if svc == nil {
				return // refused at construction
			}
			if svc.Healthy(context.Background()) {
				t.Fatal("a service built with a non-positive timeout must not report healthy")
			}
			if calls, _, _, _ := store.snapshot(); calls != 0 {
				t.Fatalf("the store must not be called without a positive bound, got %d calls", calls)
			}
		})
	}
}

func TestHealthService_NilStoreIsRejected(t *testing.T) {
	var svc *HealthService
	func() {
		defer func() { _ = recover() }()
		svc = NewHealthService(nil, healthTestTimeout)
	}()
	if svc == nil {
		return // refused at construction
	}
	healthy := func() (healthy bool) {
		defer func() {
			if recover() != nil {
				healthy = false // failing loudly is also closed
			}
		}()
		return svc.Healthy(context.Background())
	}()
	if healthy {
		t.Fatal("a service without a store must not report healthy")
	}
}
