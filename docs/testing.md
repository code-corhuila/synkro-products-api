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

`test/products-db/` is a vendored copy of `synkro-products-db`'s
migrations (that repository is private, so CI cannot check it out). After
a migration merges there, refresh the copy with
`scripts/sync-products-db-migrations.sh` and commit it;
`scripts/sync-products-db-migrations.sh --check` reports drift.
