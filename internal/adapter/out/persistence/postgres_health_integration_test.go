//go:build integration

package persistence

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
)

// Real-database tests for (*Postgres).Check. They never skip: a missing
// database is a failure, otherwise the obligation would silently not be
// verified. A fake query function is deliberately not an option here.
//
// TEST_DATABASE_URL  - products_schema as the migrations create it, the same
//                      database and role the other integration tests use.
// TEST_ADMIN_DATABASE_URL - a role allowed CREATE DATABASE and CREATE ROLE,
//                      for the fault-injection lifecycle only. It creates and
//                      drops one uniquely named database and role.

var _ out.StoreHealth = (*Postgres)(nil)

const healthBound = 5 * time.Second // proves "returns", not performance

func openHealthDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Fatal("TEST_DATABASE_URL is required: Postgres.Check must be proven against a real database")
	}
	return openDB(t)
}

func checkWithin(t *testing.T, db *Postgres, timeout time.Duration) (error, time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	start := time.Now()
	err := db.Check(ctx)
	return err, time.Since(start)
}

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

func TestPostgresHealth_RealOwnSchemaIsHealthy(t *testing.T) {
	pool := openHealthDB(t)
	// The check must be about the real own table: prove it is there first.
	var n int
	if err := pool.QueryRow(ctxT(), `SELECT count(*) FROM products_schema.product`).Scan(&n); err != nil {
		t.Fatalf("precondition, products_schema.product must be readable: %v", err)
	}

	if err, _ := checkWithin(t, NewPostgres(pool), 2*time.Second); err != nil {
		t.Fatalf("Check against a healthy database: %v", err)
	}
}

func TestPostgresHealth_AlreadyCancelledContextFails(t *testing.T) {
	db := NewPostgres(openHealthDB(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := db.Check(ctx)

	if err == nil {
		t.Fatal("Check must fail when its context is already cancelled")
	}
	if elapsed := time.Since(start); elapsed > healthBound {
		t.Fatalf("Check took %v with a cancelled context", elapsed)
	}
}

func TestPostgresHealth_LockedTableIsBoundedByTheContextThenRecovers(t *testing.T) {
	pool := openHealthDB(t)
	db := NewPostgres(pool)

	locker, err := pool.Begin(ctxT())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = locker.Rollback(ctxT()) })
	if _, err := locker.Exec(ctxT(), `LOCK TABLE products_schema.product IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatalf("locking products_schema.product: %v", err)
	}

	err, elapsed := checkWithin(t, db, 150*time.Millisecond)
	if err == nil {
		t.Fatal("Check must not report healthy while the own table cannot be read")
	}
	if elapsed > healthBound {
		t.Fatalf("Check waited %v on a locked table; the context must bound the I/O", elapsed)
	}

	if err := locker.Rollback(ctxT()); err != nil {
		t.Fatal(err)
	}
	if err, _ := checkWithin(t, db, 2*time.Second); err != nil {
		t.Fatalf("Check after the lock was released (a stuck connection?): %v", err)
	}
}

func TestPostgresHealth_ClosedPoolFails(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("TEST_DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctxT(), url)
	if err != nil {
		t.Fatal(err)
	}
	if err, _ := checkWithin(t, NewPostgres(pool), 2*time.Second); err != nil {
		t.Fatalf("precondition: the pool must work before it is closed: %v", err)
	}

	pool.Close()
	err, elapsed := checkWithin(t, NewPostgres(pool), 2*time.Second)

	if err == nil {
		t.Fatal("Check on a closed pool must fail")
	}
	if elapsed > healthBound {
		t.Fatalf("Check on a closed pool took %v", elapsed)
	}
}

func TestPostgresHealth_UnreachableServerFailsWithinTheContextAndDoesNotLog(t *testing.T) {
	logs := &safeBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	// Nothing listens on port 1: the credentials are fake and the pool
	// connects lazily, so only Check itself can discover the outage.
	pool, err := pgxpool.New(ctxT(), "postgres://svc_user:SENTINEL-PASSWORD@127.0.0.1:1/products?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	err, elapsed := checkWithin(t, NewPostgres(pool), 300*time.Millisecond)

	if err == nil {
		t.Fatal("Check against an unreachable server must fail")
	}
	if elapsed > healthBound {
		t.Fatalf("Check took %v against an unreachable server", elapsed)
	}
	if logs.Len() != 0 {
		t.Fatalf("Check must return its error, not log it; it logged %d bytes", logs.Len())
	}
}

// healthFixture is a throwaway database and a login role owned by the test.
// Roles are cluster-global, so both carry a random name and both are removed
// in Cleanup; a failed removal is reported, never ignored.
type healthFixture struct {
	t        *testing.T
	admin    *pgx.Conn // connected to the fixture database
	pool     *pgxpool.Pool
	database string
	role     string
}

func randomName(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return "pro15_health_" + hex.EncodeToString(raw)
}

func newHealthFixture(t *testing.T) *healthFixture {
	t.Helper()
	url := os.Getenv("TEST_ADMIN_DATABASE_URL")
	if url == "" {
		t.Fatal("TEST_ADMIN_DATABASE_URL is required: missing schema, denied access and access loss can only be injected with an administrator")
	}
	ctx := context.Background()
	base, err := pgx.ParseConfig(url)
	if err != nil {
		t.Fatal("TEST_ADMIN_DATABASE_URL is not a valid connection string")
	}
	f := &healthFixture{t: t, database: randomName(t), role: randomName(t)}
	password := randomName(t) + randomName(t)

	boot, err := pgx.ConnectConfig(ctx, base)
	if err != nil {
		t.Fatalf("connecting as administrator: %v", err)
	}
	t.Cleanup(func() {
		// Registered first, so it runs last: after the fixture connections
		// are closed and nothing keeps the database busy.
		defer boot.Close(ctx)
		if _, err := boot.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{f.database}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("dropping the fixture database %s: %v", f.database, err)
		}
		if _, err := boot.Exec(ctx, "DROP ROLE IF EXISTS "+pgx.Identifier{f.role}.Sanitize()); err != nil {
			t.Errorf("dropping the fixture role %s: %v", f.role, err)
		}
	})
	if _, err := boot.Exec(ctx, "CREATE ROLE "+pgx.Identifier{f.role}.Sanitize()+" LOGIN PASSWORD '"+password+"'"); err != nil {
		t.Fatalf("creating the fixture role: %v", err)
	}
	if _, err := boot.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{f.database}.Sanitize()); err != nil {
		t.Fatalf("creating the fixture database: %v", err)
	}

	adminCfg := base.Copy()
	adminCfg.Database = f.database
	f.admin, err = pgx.ConnectConfig(ctx, adminCfg)
	if err != nil {
		t.Fatalf("connecting to the fixture database: %v", err)
	}
	t.Cleanup(func() { f.admin.Close(ctx) })

	poolCfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal("TEST_ADMIN_DATABASE_URL is not a valid connection string")
	}
	poolCfg.ConnConfig.Database = f.database
	poolCfg.ConnConfig.User = f.role
	poolCfg.ConnConfig.Password = password
	f.pool, err = pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.pool.Close)
	return f
}

func (f *healthFixture) exec(sql string) {
	f.t.Helper()
	if _, err := f.admin.Exec(context.Background(), sql); err != nil {
		f.t.Fatalf("fixture statement failed: %v", err)
	}
}

func (f *healthFixture) roleIdent() string { return pgx.Identifier{f.role}.Sanitize() }

// TestPostgresHealth_FaultInjectionLifecycle walks one database through the
// states the health check must tell apart. Each step depends on the previous
// one, so the subtests are sequential and not independent.
func TestPostgresHealth_FaultInjectionLifecycle(t *testing.T) {
	f := newHealthFixture(t)
	db := NewPostgres(f.pool)
	mustFail := func(t *testing.T, what string) {
		t.Helper()
		err, elapsed := checkWithin(t, db, 2*time.Second)
		if err == nil {
			t.Fatalf("Check reported healthy: %s", what)
		}
		if elapsed > healthBound {
			t.Fatalf("Check took %v: %s", elapsed, what)
		}
	}

	t.Run("missing schema is unhealthy", func(t *testing.T) {
		mustFail(t, "products_schema does not exist")
	})

	t.Run("access denied is unhealthy", func(t *testing.T) {
		f.exec(`CREATE SCHEMA products_schema`)
		f.exec(`CREATE TABLE products_schema.product (product_id uuid PRIMARY KEY)`)
		mustFail(t, "the role has no privileges on products_schema")
	})

	t.Run("empty own table is healthy", func(t *testing.T) {
		f.exec(`GRANT USAGE ON SCHEMA products_schema TO ` + f.roleIdent())
		f.exec(`GRANT SELECT ON products_schema.product TO ` + f.roleIdent())
		if err, _ := checkWithin(t, db, 2*time.Second); err != nil {
			t.Fatalf("an empty product table is a healthy store: %v", err)
		}
	})

	t.Run("access loss is unhealthy", func(t *testing.T) {
		f.exec(`REVOKE SELECT ON products_schema.product FROM ` + f.roleIdent())
		mustFail(t, "SELECT was revoked from a live pool")
	})

	t.Run("regained access is healthy again", func(t *testing.T) {
		f.exec(`GRANT SELECT ON products_schema.product TO ` + f.roleIdent())
		if err, _ := checkWithin(t, db, 2*time.Second); err != nil {
			t.Fatalf("Check after access was restored: %v", err)
		}
	})

	t.Run("missing table is unhealthy", func(t *testing.T) {
		f.exec(`DROP TABLE products_schema.product`)
		mustFail(t, "products_schema.product was dropped, a bare SELECT 1 would still pass")
	})
}
