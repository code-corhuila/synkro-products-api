# synkro-products-api

> products bounded context: service API

Part of the **SynkroTech SAS Sales Management System** — organization `code-corhuila`.
Governance and documentation live in [`synkro-docs`](https://github.com/code-corhuila/synkro-docs).

## Running locally

The service needs a PostgreSQL instance with `products_schema` created by
[`synkro-products-db`](https://github.com/code-corhuila/synkro-products-db)'s
migrations, reached as the `products_app` role:

```bash
DATABASE_URL="postgres://products_app:<password>@localhost:5432/postgres?sslmode=disable" go run ./cmd/products-api
```

The service listens on `:8080` (override with `HTTP_PORT`). It refuses to
start without `DATABASE_URL`. Verify it:

```bash
curl http://localhost:8080/health
```

## Running the tests

Unit (domain, use cases) and HTTP tests need nothing but Go:

```bash
go test ./...
```

The PostgreSQL integration tests in `internal/adapter/out/persistence/`
are skipped unless `TEST_DATABASE_URL` is set. To run them locally:

```bash
docker network create synkro-it
docker run -d --name synkro-it-pg --network synkro-it -p 55432:5432 -e POSTGRES_PASSWORD=it_admin_pw postgres:16-alpine
docker exec synkro-it-pg psql -U postgres -c "CREATE ROLE products_app LOGIN PASSWORD 'it_app_pw';"
FLYWAY_URL=jdbc:postgresql://synkro-it-pg:5432/postgres FLYWAY_USER=postgres FLYWAY_PASSWORD=it_admin_pw FLYWAY_DOCKER_NETWORK=synkro-it scripts/migrate-test-db.sh
TEST_DATABASE_URL="postgres://products_app:it_app_pw@localhost:55432/postgres?sslmode=disable" go test -count=1 ./internal/adapter/out/persistence/
```

`test/products-db/` is a vendored copy of `synkro-products-db`'s
migrations (that repository is private, so CI cannot check it out). After
a migration merges there, refresh the copy with
`scripts/sync-products-db-migrations.sh` and commit it;
`scripts/sync-products-db-migrations.sh --check` reports drift.

## Branching

Three permanent branches. **None of them accepts a direct commit** — you enter through a child
branch and leave through a Pull Request.

```
develop  <--PR--  feat/... fix/... chore/...
qa       <--PR--  qa/...
main     <--PR--  release/...  hotfix/...
```

Promotion happens **by re-application** (`git cherry-pick -x`), never by merging one permanent
branch into another: `merge develop -> qa` and `merge qa -> main` do not exist in this model.

`main` requires **1 approval from `ariel5253`**. On `develop` and `qa` the team sets its own review
rule.

Full policy: `00-governance/branching-policy.md` in `synkro-docs`.
