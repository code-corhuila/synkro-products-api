# Testing

Unit (domain, use cases) and HTTP tests need nothing but Go:

```bash
go test ./...
```

The PostgreSQL integration tests in `internal/adapter/out/persistence/`
retain their existing optional unit-stage behavior. The new real health tests
use the `integration` build tag and fail if fixture URLs are missing. CI runs
all packages with that tag after migrating a real database, then enforces
statement-weighted coverage: domain 90%, application 80%, infrastructure 60%,
global 80%. The administrator URL is used only by disposable fault-injection
tests, which remove their uniquely named database and role. To run locally:

```bash
docker network create synkro-it
docker run -d --name synkro-it-pg --network synkro-it -p 55432:5432 -e POSTGRES_PASSWORD=it_admin_pw postgres:16-alpine
docker exec synkro-it-pg psql -U postgres -c "CREATE ROLE products_app LOGIN PASSWORD 'it_app_pw';"
FLYWAY_URL=jdbc:postgresql://synkro-it-pg:5432/postgres FLYWAY_USER=postgres FLYWAY_PASSWORD=it_admin_pw FLYWAY_DOCKER_NETWORK=synkro-it scripts/migrate-test-db.sh
TEST_DATABASE_URL="postgres://products_app:it_app_pw@localhost:55432/postgres?sslmode=disable" \
TEST_ADMIN_DATABASE_URL="postgres://postgres:it_admin_pw@localhost:55432/postgres?sslmode=disable" \
go test -tags=integration -count=1 -coverpkg=./... -coverprofile=coverage.out ./...
python3 scripts/check-coverage.py coverage.out
```

`test/products-db/` is a vendored copy of `synkro-products-db`'s
migrations (that repository is private, so CI cannot check it out). After
a migration merges there, refresh the copy with
`scripts/sync-products-db-migrations.sh` and commit it;
`scripts/sync-products-db-migrations.sh --check` reports drift.

## Products security testing

### Negative authentication tests

`internal/adapter/in/httpapi/security_negative_auth_test.go` (every test is
named `TestSecurityNegativeAuth_*`) sends forged credentials over real HTTP to
the price update and the stock adjustment, then reads the repository back:
an edited ADMIN payload, `alg: none`, a signature from another RSA key, a
tampered signature, an expired token and a non-RS256 algorithm must answer 401
and leave the price unchanged; a SALESPERSON stock adjustment must answer 403
and write no adjustment, stock change or idempotency key. `TestAuth_*` in
`auth_http_test.go` covers the same rejections on every route.

The CI job `security-negative-auth` runs both groups on every pull request to
`develop`, `qa` and `main`, with no path filters, and fails if the tests are
renamed away. Whether that check is *required* to merge is a branch
protection setting, not something this repository's files can enforce.

```bash
go test -count=1 -v -run '^TestSecurityNegativeAuth_|^TestAuth_' ./internal/adapter/in/httpapi/
```

### Stock adjustment author

`internal/adapter/in/httpapi/stock_adjustment_author_test.go` proves, over
real HTTP with a valid body, that `adjustedBy` of a stock adjustment is the
verified `sub` of the token: an INVENTORY token that sends another user's id
as `adjustedBy` is recorded under its own `sub`, and a token with an invalid
signature (other RSA key, tampered signature, `alg: none`, edited payload,
expired) that carries another user's `sub` answers 401 and leaves no
adjustment, no stock change and no idempotency key. The in-memory repository
is read back for the stored author.

`stock_adjustment_author_integration_test.go` (build tag `integration`) runs the
same two obligations against real PostgreSQL as `products_app` and reads
`adjusted_by`, the row counts, the stock and the idempotency key straight from
`products_schema`.

## Product price history

See [design](price-history-design.md) and [OpenAPI extension](price-history.openapi.yaml).
The history tests exercise two verified INVENTORY identities, a forged client
author and invalid signature, unchanged price, UTC time, role/permission checks,
pagination, and an actual PostgreSQL history query. The shared adapter contract
tests prove unique-id insert failure rolls back the catalog change, concurrent
writers form a contiguous price chain, and deactivation preserves the latest price
and stock. Run the integration command above after applying Products DB V021–V024.
The application now requires explicit update-unit and history-reader ports.
