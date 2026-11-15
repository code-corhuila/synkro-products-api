# Store health (HU-PRO-15, TD-017): design

Status: design/tests and inert compile-only signatures preceded implementation
in `7a516ae`. Assertion RED and final real-engine verification are below.

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
- A nil router health port fails closed with 503.
- A nil store or nonpositive timeout is a composition error rejected by the
  health-service constructor.
- A real `StoreHealth` that ignores its context is a port bug and is not worked
  around; only the parent's cancellation is checked by the use case.

## Out of scope

No business API change, new ADR, auth dependency or other-service health probe.
The vendored fixture is pinned only after verification of the owning DB delivery.

## Executed verification and traceability

- Baseline develop: `af06bb056fedb7b9f83d518436c3370a0f1373d1`.
- Tests first: `7a516ae`, with inert compile-only interfaces preserving the old
  unconditional liveness. Compilation gaps were recorded separately; real RED
  had 38 failed test/subtest events, no skips or compiler/setup failure.
  [RED record](evidence/store-health-tdd.json).
- A strict consecutive-clock comparison failed on Windows because two calls
  shared one clock tick. The test now proves distinct derived contexts, correct
  deadline ordering and cancellation after return; the production code did not
  change for this adjustment. All 44 health test/subtest events then passed.
- Complete real-PostgreSQL suite on V020: 282 top-level tests,
  421 passing test/subtest events, zero failures/skips. Go 1.26.9.
  Coverage includes calls across packages and merges repeated profile blocks:
  domain 100%, application 90.4%, infrastructure 91.42%, global 89.9%.
  Build, vet with integration tag and domain architecture check passed.
  [Verification](evidence/store-health-verification.json).
- A running compiled API completed 15 HTTP checks: create/replay reservation,
  frozen price after catalog update, duplicate request rejection, release/replay
  with stock restored; then actual PostgreSQL stop produced 503 and restart
  restored 200 in the same API process. No response/log credential leakage.
  [Process evidence](evidence/process-outage-and-reservations.json).
- Two earlier process runs exposed fixture limitations (tmpfs data loss, then
  Docker's reassigned random host port). They were not API failures or counted
  as successful recovery. The passing run used disposable storage and a fixed
  loopback port. A coverage run against the retired fixture failed setup and
  was repeated on the restored migrated fixture; final evidence is explicit.

| Requirements | Story | Tests | Code |
|---|---|---|---|
| NFR-002/006 | HU-PRO-15 | TestHealthService_* | application/port/{in,out}/health.go, usecase/health.go |
| NFR-002/004 | HU-PRO-15 | TestStoreHealthHTTP_* | httpapi/handler.go, cmd/products-api/main.go |
| NFR-002/009 | HU-PRO-15 | TestPostgresHealth_*, real outage | persistence/health.go |
| FR-004/007, NFR-009 | HU-PRO-15 | existing real reservation tests, process lifecycle | unchanged reservation code + pinned V018..V020 fixture |

Sources: [cross-cutting health design](https://github.com/code-corhuila/synkro-docs/blob/main/05-architecture/cross-cutting.md),
[Products OpenAPI](https://github.com/code-corhuila/synkro-docs/blob/main/07-api/contracts/openapi/synkro-products-api.yaml),
and [HU-PRO-15](https://github.com/code-corhuila/synkro-docs/issues/317).
The accepted HealthResponse already permits dependencies.database; no contract
change is needed. Domain, authorization and reservation semantics are unchanged.

The central documentation reconciliation remains pending: the project owner
explicitly excluded execution of HU-DOCS-98. No QA/main rollout or central debt
closure is claimed by these repository-local results.
