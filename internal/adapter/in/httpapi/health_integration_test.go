//go:build integration

package httpapi

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/code-corhuila/synkro-products-api/internal/adapter/out/persistence"
	"github.com/code-corhuila/synkro-products-api/internal/application/usecase"
)

// Real HTTP server, real PostgreSQL adapter and real health service. Unlike
// the older integration tests these never skip: a missing database is a
// failure, otherwise the obligation would be silently not verified.

func openHealthPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("TEST_DATABASE_URL is required: the store health check must be proven against a real database")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return pool
}

func realHealthAPI(t *testing.T, pool *pgxpool.Pool, timeout time.Duration) *testAPI {
	t.Helper()
	return newTestAPIWithHealth(t, usecase.NewHealthService(persistence.NewPostgres(pool), timeout))
}

func TestStoreHealthHTTP_RealPostgresReportsOk(t *testing.T) {
	api := realHealthAPI(t, openHealthPool(t), time.Second)
	before := time.Now()

	status, _, body := getHealth(t, api.srv.URL)

	if status != http.StatusOK {
		t.Fatalf("status code = %d, want 200 (body %s)", status, body)
	}
	assertHealthBody(t, body, "ok", "ok", before)
}

func TestStoreHealthHTTP_RealPostgresUnavailableAnswers503(t *testing.T) {
	pool := openHealthPool(t)
	api := realHealthAPI(t, pool, time.Second)
	if status, _, body := getHealth(t, api.srv.URL); status != http.StatusOK {
		t.Fatalf("precondition: expected 200 while the database is up, got %d (%s)", status, body)
	}

	pool.Close() // the connection to the database is really gone
	before := time.Now()
	status, _, body := getHealth(t, api.srv.URL)

	if status != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want 503 (body %s)", status, body)
	}
	if elapsed := time.Since(before); elapsed > 5*time.Second {
		t.Fatalf("the unavailable probe took %v", elapsed)
	}
	assertHealthBody(t, body, "degraded", "down", before)
}

func TestStoreHealthHTTP_RealPostgresLockedTableAnswers503ThenRecovers(t *testing.T) {
	pool := openHealthPool(t)
	api := realHealthAPI(t, pool, 150*time.Millisecond)

	ctx := context.Background()
	locker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = locker.Rollback(ctx) })
	if _, err := locker.Exec(ctx, `LOCK TABLE products_schema.product IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatalf("locking products_schema.product: %v", err)
	}

	before := time.Now()
	status, _, body := getHealth(t, api.srv.URL)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d while the table is locked, want 503 (body %s)", status, body)
	}
	if elapsed := time.Since(before); elapsed > 5*time.Second {
		t.Fatalf("the probe waited %v on a locked table; it must give up at its timeout", elapsed)
	}

	if err := locker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if status, _, body := getHealth(t, api.srv.URL); status != http.StatusOK {
		t.Fatalf("status code = %d after the lock was released, want 200 (body %s)", status, body)
	}
}
