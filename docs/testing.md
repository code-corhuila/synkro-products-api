# Products security testing

## Negative authentication tests

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

## Stock adjustment author

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

`test/products-db/` is a vendored copy of `synkro-products-db`'s
migrations (that repository is private, so CI cannot check it out). After
a migration merges there, refresh the copy with
`scripts/sync-products-db-migrations.sh` and commit it;
`scripts/sync-products-db-migrations.sh --check` reports drift.


## Product price history

See [design](price-history-design.md) and [OpenAPI extension](price-history.openapi.yaml).
The history tests exercise two verified INVENTORY identities, a forged client
author and invalid signature, unchanged price, UTC time, role/permission checks,
pagination, and an actual PostgreSQL history query. The shared adapter contract
tests prove unique-id insert failure rolls back the catalog change, concurrent
writers form a contiguous price chain, and deactivation preserves the latest price
and stock. Run the integration command above after applying Products DB V021–V024.
The application now requires explicit update-unit and history-reader ports.
