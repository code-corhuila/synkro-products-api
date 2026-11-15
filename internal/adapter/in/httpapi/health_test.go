package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/in"
	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/application/usecase"
)

var (
	_ in.HealthUseCases = healthyFixture{}
	_ in.HealthUseCases = (*healthStub)(nil)
	_ out.StoreHealth   = failingStore{}
	_ out.StoreHealth   = blockingStore{}
)

// healthyFixture is the explicit healthy dependency every pre-existing test
// gets through newTestAPI: NewRouter has no default.
type healthyFixture struct{}

func (healthyFixture) Healthy(context.Context) bool { return true }

type healthStub struct {
	mu      sync.Mutex
	healthy bool
	calls   int
	ctx     context.Context
}

func (s *healthStub) Healthy(ctx context.Context) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.ctx = ctx
	return s.healthy
}

type failingStore struct{ err error }

func (s failingStore) Check(context.Context) error { return s.err }

type blockingStore struct{}

func (blockingStore) Check(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

type syncLogs struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *syncLogs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *syncLogs) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// captureLogs routes slog and the log package to a buffer; such tests must
// not run in parallel.
func captureLogs(t *testing.T) *syncLogs {
	t.Helper()
	logs := &syncLogs{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return logs
}

func getHealth(t *testing.T, baseURL string, headers ...string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, body
}

type healthCase struct {
	name       string
	healthy    bool
	wantStatus int
	wantState  string
	wantDB     string
}

var healthCases = []healthCase{
	{"healthy", true, http.StatusOK, "ok", "ok"},
	{"unhealthy", false, http.StatusServiceUnavailable, "degraded", "down"},
}

// assertHealthBody checks the documented schema exactly: four top-level
// fields, dependencies holding only database, a UTC timestamp.
func assertHealthBody(t *testing.T, body []byte, wantState, wantDB string, notBefore time.Time) {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		t.Fatalf("health body is not a JSON object: %q", body)
	}
	if len(top) != 4 {
		t.Fatalf("health body must have exactly status, service, timestamp and dependencies, got %s", body)
	}
	for _, field := range []string{"status", "service", "timestamp", "dependencies"} {
		if _, ok := top[field]; !ok {
			t.Fatalf("health body lacks %q: %s", field, body)
		}
	}
	var status, service, stamp string
	for field, into := range map[string]*string{"status": &status, "service": &service, "timestamp": &stamp} {
		if err := json.Unmarshal(top[field], into); err != nil {
			t.Fatalf("%s is not a string: %s", field, top[field])
		}
	}
	if status != wantState {
		t.Errorf("status = %q, want %q", status, wantState)
	}
	if service != "synkro-products-api" {
		t.Errorf("service = %q, want synkro-products-api", service)
	}
	parsed, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		t.Fatalf("timestamp %q is not RFC 3339: %v", stamp, err)
	}
	if !strings.HasSuffix(stamp, "Z") {
		t.Errorf("timestamp %q is not UTC", stamp)
	}
	if parsed.Before(notBefore.Add(-time.Second)) || parsed.After(time.Now().Add(time.Second)) {
		t.Errorf("timestamp %v is not the time of the request (after %v)", parsed, notBefore)
	}
	var deps map[string]string
	if err := json.Unmarshal(top["dependencies"], &deps); err != nil {
		t.Fatalf("dependencies must be an object of strings: %s", top["dependencies"])
	}
	if len(deps) != 1 || deps["database"] != wantDB {
		t.Errorf("dependencies = %v, want only database=%q", deps, wantDB)
	}
}

func TestStoreHealthHTTP_ResponseSchema(t *testing.T) {
	for _, tc := range healthCases {
		t.Run(tc.name, func(t *testing.T) {
			api := newTestAPIWithHealth(t, &healthStub{healthy: tc.healthy})
			before := time.Now()

			status, header, body := getHealth(t, api.srv.URL)

			if status != tc.wantStatus {
				t.Fatalf("status code = %d, want %d (body %s)", status, tc.wantStatus, body)
			}
			if ct := header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			assertHealthBody(t, body, tc.wantState, tc.wantDB, before)
		})
	}
}

func TestStoreHealthHTTP_IsPublicWhateverTheCredentials(t *testing.T) {
	credentials := map[string][]string{
		"no token":        nil,
		"malformed token": {"Authorization", "Bearer not-a-real-token"},
		"wrong scheme":    {"Authorization", "Basic dXNlcjpwYXNz"},
	}
	for _, tc := range healthCases {
		for name, headers := range credentials {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				api := newTestAPIWithHealth(t, &healthStub{healthy: tc.healthy})

				status, header, _ := getHealth(t, api.srv.URL, headers...)

				if status != tc.wantStatus {
					t.Fatalf("status code = %d, want %d: the probe must not ask for authentication", status, tc.wantStatus)
				}
				if header.Get("WWW-Authenticate") != "" {
					t.Error("the probe must not challenge for credentials")
				}
			})
		}
	}
}

func TestStoreHealthHTTP_ForwardsTheRequestContext(t *testing.T) {
	stub := &healthStub{healthy: true}
	api := newTestAPIWithHealth(t, stub)

	getHealth(t, api.srv.URL, "X-Correlation-Id", "health-trace-1")

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.calls != 1 {
		t.Fatalf("the handler must consume the health port once per request, got %d", stub.calls)
	}
	// The correlation id is stored in the request context by middleware: it
	// can only reach the port through that very context.
	probe := httptest.NewRequest(http.MethodGet, "/health", nil).WithContext(stub.ctx)
	if got := traceIDFrom(probe); got != "health-trace-1" {
		t.Fatalf("the port did not receive the request context (trace id %q)", got)
	}
}

// NewRouter has no healthy default: a missing port is refused at wiring or
// reported unhealthy, never reported as ok.
func TestStoreHealthHTTP_NilPortFailsClosed(t *testing.T) {
	var api *testAPI
	func() {
		defer func() { _ = recover() }()
		api = newTestAPIWithHealth(t, nil)
	}()
	if api == nil {
		return // refused while wiring the router
	}

	req, _ := http.NewRequest(http.MethodGet, api.srv.URL+"/health", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return // the request failed; a server that fails the call is closed as well
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("a router without a health port answered 200: %s", body)
	}
	if resp.StatusCode == http.StatusServiceUnavailable {
		assertHealthBody(t, body, "degraded", "down", time.Now().Add(-time.Minute))
	}
}

func TestStoreHealthHTTP_SlowStoreAnswers503WithinTheServiceTimeout(t *testing.T) {
	api := newTestAPIWithHealth(t, usecase.NewHealthService(blockingStore{}, 30*time.Millisecond))
	before := time.Now()

	status, _, body := getHealth(t, api.srv.URL)

	if status != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want 503 (body %s)", status, body)
	}
	if elapsed := time.Since(before); elapsed > 5*time.Second {
		t.Fatalf("the probe took %v although its timeout is 30ms", elapsed)
	}
	assertHealthBody(t, body, "degraded", "down", before)
}

func TestStoreHealthHTTP_FailureDetailsNeverReachResponseOrLogs(t *testing.T) {
	const dsn = "postgres://svc_user:SENTINEL-PASSWORD@db.internal:5432/products"
	secrets := []string{"SENTINEL-PASSWORD", "svc_user", "db.internal", "connection refused"}
	logs := captureLogs(t)
	store := failingStore{err: errors.New("dial " + dsn + ": connection refused")}
	api := newTestAPIWithHealth(t, usecase.NewHealthService(store, time.Second))

	status, header, body := getHealth(t, api.srv.URL)

	if status != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want 503", status)
	}
	assertHealthBody(t, body, "degraded", "down", time.Now().Add(-time.Minute))

	var everything strings.Builder
	everything.Write(body)
	for name, values := range header {
		everything.WriteString(name + ": " + strings.Join(values, ",") + "\n")
	}
	everything.WriteString(logs.String())
	for _, secret := range secrets {
		if strings.Contains(everything.String(), secret) {
			t.Fatalf("%q leaks through the response or the logs:\n%s", secret, everything.String())
		}
	}
}
