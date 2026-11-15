# Store health (HU-PRO-15, TD-017): design

Status: design written before code. Nothing described here has been executed;
see "Pending executions".

## Behavior

`GET /health` is public (no token, no versioned path) and reports only this
service's own store. It calls no other service.

| State | HTTP | Body |
|---|---|---|
| healthy | 200 | `status: "ok"`, `dependencies.database: "ok"` |
| unhealthy | 503 | `status: "degraded"`, `dependencies.database: "down"` |

The body is exactly four fields: `status`, `service` (`synkro-products-api`),
`timestamp` (RFC 3339, UTC) and `dependencies` (only `database`). No error,
credential, DSN, driver text or detail appears in the response or in logs.

## Architecture: Ports and Adapters + Health Check

The domain model is unchanged and imports no infrastructure. Ports are
interfaces; the application owns the orchestration; the PostgreSQL adapter owns
the I/O; the root wires it.

| Piece | Location | Signature |
|---|---|---|
| outbound port | `internal/application/port/out/health.go` | `StoreHealth interface { Check(context.Context) error }` |
| inbound port | `internal/application/port/in/health.go` | `HealthUseCases interface { Healthy(context.Context) bool }` |
| use case | `internal/application/usecase/health.go` | `NewHealthService(out.StoreHealth, time.Duration) *HealthService` |
| adapter | `persistence` | `(*Postgres).Check(context.Context) error` |
| inbound adapter | `httpapi` | `NewRouter(...)` gains a REQUIRED sixth argument `in.HealthUseCases` |
| composition | `cmd/products-api/main.go` | `NewHealthService(db, time.Second)` |

- Each check gets a derived timeout and requires a positive duration. The
  parent's cancellation and shorter deadline are preserved.
- A port honors its context. The use case does not start goroutines to
  accommodate a port that ignores it.
- `Healthy` returns a bool, so error detail cannot leave the use case. The
  adapter returns its error only to the use case and never logs it.
- `Check` reads the own table `products_schema.product`, not just `SELECT 1`: a
  missing schema or table, or denied access, is unhealthy. An empty table is
  healthy.
- `NewRouter` has no default: a missing port is refused when wiring or reported
  unhealthy, never "ok".

## Test map

| Requirement | Test |
|---|---|
| port called once, derived deadline, per-call deadline | `usecase/health_test.go`: `TestHealthService_*` |
| parent deadline/cancellation, slow store, cancelled parent | same file |
| no leak to logs, non-positive timeout, nil store | same file |
| exact schema for 200/503, UTC timestamp, one dependency | `httpapi/health_test.go`: `TestStoreHealthHTTP_ResponseSchema` |
| public with no or malformed token | `TestStoreHealthHTTP_IsPublicWhateverTheCredentials` |
| request context forwarded | `TestStoreHealthHTTP_ForwardsTheRequestContext` |
| nil port fails closed | `TestStoreHealthHTTP_NilPortFailsClosed` |
| slow store, secrets absent from response and logs | `TestStoreHealthHTTP_SlowStore...`, `..._FailureDetails...` |
| real server + real Postgres, closed pool, locked table | `httpapi/health_integration_test.go` |
| real adapter: healthy, cancelled, locked, closed, unreachable | `persistence/postgres_health_integration_test.go` |
| missing schema, denied, empty healthy, access loss, missing table | `TestPostgresHealth_FaultInjectionLifecycle` |

Existing tests are preserved. `newTestAPI` in `catalog_test.go` is the only
shared constructor changed: it now supplies an explicit `healthyFixture`.

## Methods and limits

- Persistence and real-HTTP tests need a database and never skip: a missing
  `TEST_DATABASE_URL` is a failure. The fault-injection lifecycle also needs
  `TEST_ADMIN_DATABASE_URL`, a role allowed `CREATE DATABASE`/`CREATE ROLE`. It
  creates one uniquely named database and role and removes them in `Cleanup`;
  roles are cluster-global, so a failed removal is reported as a test error.
- Tests avoid sleeps; the only bounds are generous "it returns" limits (5 s)
  against 30-150 ms timeouts. They are not performance assertions.
- A closed pool, a locked table and a closed port stand in for outages. Stopping
  and restarting a real database under a running API process is done later by
  hand and is not part of these tests.
- Not specified: whether a nil port is refused at wiring or answers 503. The
  tests accept either, and reject a 200.
- Not specified: whether a non-positive timeout panics at construction or fails
  closed at check time. Same rule.
- A real `StoreHealth` that ignores its context is a port bug and is not worked
  around; only the parent's cancellation is checked by the use case.

## Out of scope

No other API change, no new ADR, no auth or other-service dependency, no CI or
README edit yet, and no copy of the new migrations into `test/products-db`
until the owning DB delivery is verified.

## Pending executions

Nothing here has been run: not the compile (the ports, service and router
argument do not exist yet), not RED, not the integration tests, not the real
outage run. They are executed and recorded after this document.
