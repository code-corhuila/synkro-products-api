# Products API — main promotion proposal (HU-PRO-15)

## Purpose

Proposal to promote the Products API store-health delivery of HU-PRO-15
(synkro-docs#317) from `qa` to `main`, requested by the owner after both QA
PR24 deliveries were merged. The approved scope is narrow: TD017 own-store
health, with its fixture consuming the paired DB delivery (TD018).
This is a local verification of a proposal, plus a provenance-only update after
the paired DB `main` merge. It is **not** an actual merge of the API into
`main`, **not** a persistent deployment and **not** a tag. Historical
QA/develop execution evidence and trails stay separate and unchanged; only
their current-status links are updated.

## Source and base

| Item | Commit |
| --- | --- |
| Source (actual integrated QA merge of API PR24) | `4f0f15be88156ed86ec6755d679721ea026d71f0` |
| Main base | `8c04934ae5b51417a0b88506cc59734ef7913e64` |
| Previous QA head | `22245634d2f10a5667899a24d5c1511d4f3a0891` |
| Candidate (tested) | `47c8e9dbe8e1e8240d7c7376ff1bcaadcdc6cd5e` |
| Fixture `SOURCE_COMMIT` pin, current (actual DB `main` merge, DB PR25) | `9c52d7761d8a32f679261f71c76528f0b0ec9bd7` |
| Fixture `SOURCE_COMMIT` pin, historical (actual integrated DB QA; pin of candidate `47c8e9db…` and of its validation run) | `89cd9a670d3e540c3cc524411ff77c058f1b8960` |
| Branch | `release/products-store-health` |

The API QA merge was actually performed by the owner on 2026-11-14 21:34:11
America/Bogota; the DB QA merge (`89cd9a67…`) on 2026-11-14 21:27:34. The DB
`main` merge (DB PR25, `9c52d776…`) was merged on 2026-11-14 21:53:57
America/Bogota; evidence:
[`docs/evidence/main-db-integration.json`](evidence/main-db-integration.json).
API `main` PR25 is still open: no actual API `main` merge SHA exists and none
is claimed. No merge between permanent
branches was made and nothing was committed directly to a permanent branch; all
six source commits were reapplied on the release branch with `cherry-pick -x`.
Policy:
[branching policy](https://github.com/code-corhuila/synkro-docs/blob/main/00-governance/branching-policy.md).

## Source → main trail

All six sources are actual ancestors of the integrated QA commit. Each target
commit message carries the literal line shown.

1. `17e271c8f5b20596d83ccafb2adb879e69da009f` →
   `93be6cd620a8cb9659868a9e4d1a865695832af0`
   (`test(health): specify bounded store probe before implementation`)

   `(cherry picked from commit 17e271c8f5b20596d83ccafb2adb879e69da009f)`

2. `aceb430c47a70a647f93efe7ae5664b56a42455f` →
   `49e8d43fcdeb2622ba7e9304cce052bffbdd7799`
   (`feat(health): report bounded Products store availability`)

   `(cherry picked from commit aceb430c47a70a647f93efe7ae5664b56a42455f)`

3. `936ed4480694dc37aaacef7820bedeebfdd6e043` →
   `73ce87d7cab8ea4ddf76f19f994d39eaafd4c252`
   (`docs(health): record complete Linux validation and delivery gates`)

   `(cherry picked from commit 936ed4480694dc37aaacef7820bedeebfdd6e043)`

4. `e46ac534638e34ec56be55ab14107ce93f2599ac` →
   `a4a1bc32c347e48c0c7672aff438d58e4fa7a33d`
   (`chore(test): pin merged Products DB revision`)

   `(cherry picked from commit e46ac534638e34ec56be55ab14107ce93f2599ac)`

5. `5f6e1095350e2b873ff3f0797090ce011948b209` →
   `7037e947c2463d9e765bfba31a7ba4177a179e14`
   (`docs(qa): record Products API promotion evidence`)

   `(cherry picked from commit 5f6e1095350e2b873ff3f0797090ce011948b209)`

6. `f04b0c4952b2e197f8b6af44ef8feba8e2781b88` →
   `47c8e9dbe8e1e8240d7c7376ff1bcaadcdc6cd5e`
   (`chore(qa): pin integrated Products DB schema`)

   `(cherry picked from commit f04b0c4952b2e197f8b6af44ef8feba8e2781b88)`

## Candidate and protected content

The candidate tree is identical to the tree of integrated QA
`4f0f15be88156ed86ec6755d679721ea026d71f0`, before this documentation was
added. No new production code, configuration, schema, test, credential or
`.github` change is made. Added on top of the tested candidate: this report, the
verification JSON, the DB integration JSON, the `SOURCE_COMMIT` pin update and
current-status links in the QA report and health design. CODEOWNERS is unchanged.

`test/products-db/SOURCE_COMMIT` now pins the actual DB `main` merge
`9c52d7761d8a32f679261f71c76528f0b0ec9bd7` (DB PR25), replacing the actual
integrated DB QA commit `89cd9a670d3e540c3cc524411ff77c058f1b8960`, which
remains the historical pin of the tested candidate. Per
`docs/evidence/main-db-integration.json`, the DB merge tree equals the tree of
published DB head `0fde8170f92cf377e6cb103e3e65bd050b008d5c`; all 38
vendored migration/config/placeholder blobs are identical; only 3 docs differ
from the tested DB candidate `5030b3d490d046d9abd4837b0240f2c40e9a8e0d`.

Current scope of this change: documentation, `SOURCE_COMMIT` provenance and the
new JSON evidence only. No SQL, Go, test or runtime configuration changed and
no deployment is performed by this task.

## Historical validation (local Linux, on the candidate)

This is the run on API candidate `47c8e9dbe8e1e8240d7c7376ff1bcaadcdc6cd5e` with
the then-current fixture pin `89cd9a670d3e540c3cc524411ff77c058f1b8960`. No new
test run was made after the DB `main` merge: the SQL, Go, tests and runtime
configuration are identical, so this result is preserved, not re-executed.

Evidence: `docs/evidence/main-promotion-verification.json`. Run on
`golang:1.26.9-alpine3.23` (Go 1.26.9), 2026-11-14 21:37–21:38 America/Bogota (JSON timestamps are UTC), API commit
`47c8e9dbe8e1e8240d7c7376ff1bcaadcdc6cd5e` with DB fixture
`89cd9a670d3e540c3cc524411ff77c058f1b8960`; passed. QA and develop evidence is
not claimed as newly executed.

- Stages: common files, build, vet (including integration), unit, domain
  architecture, integration.
- Migrations: empty vendored V001..V020 applied (20 versions) on real
  PostgreSQL.
- Unit: 219 top-level tests; 350 passing test/subtest events; 56 skipped events
  (legacy DB-dependent tests, unit stage only); no failures.
- Integration (real PostgreSQL): 282 top-level tests; 421 passing test/subtest
  events; 0 skips; no failures.
- Coverage (statement-weighted; all four gates pass): domain 100% (46/46, min
  90), application 90.4% (160/177, min 80), infrastructure 91.42% (991/1084,
  min 60), global 89.9% (1219/1356, min 80).
- Format: all 70 committed LF Go files pass `gofmt`.
- Fixture: all 38 vendored migration/config/placeholder blobs match the release
  DB candidate.
- No owned containers or networks remain.

No new compiled-process stop/restart run was made for `main`. The earlier
15-check process run (`docs/evidence/process-outage-and-reservations.json`)
remains develop history only; the `main` candidate health HTTP and PostgreSQL
integration tests were executed in the historical run above.

## Pact scope

The [testing strategy, Tier 3b](https://github.com/code-corhuila/synkro-docs/blob/main/11-quality/testing-strategy.md#tier-3b-pact)
unchanged-call rule applies.

- This change introduces no outgoing HTTP client or call and does not change
  business-route consumer serialization. `/health` implements the existing
  documented 200/503 `HealthResponse`.
- The bounded Pact matrix currently covers Customers/Sales; no Products fixture
  exists. No new or regenerated consumer Pact is required.
- No Pact was executed and no Pact result, waiver or green status is claimed.

## Billing exception

The owner decided on 2026-11-14 (America/Bogota) that the GitHub Actions
account-billing failure is not an impediment for this same delivery only. Jobs
that never started remain unstarted and are not successful tests; no green
remote CI is claimed. No other review, integrity, test or branch gate is
removed.

## Order, review and remaining evidence

- The owner alone merges. The DB `main` PR25 was merged first (see above) and
  the API fixture provenance now points to that actual DB `main` merge. The
  API `main` PR25 remains pending its final review and owner merge.
- `main` requires current approval by ariel5253 on the final API head, plus the
  existing conversation/review rules; that approval is pending. The DB review
  snapshot is `[]`: no ariel5253 approval is recorded and none is inferred from
  the DB merge. QA/develop approvals do not transfer and CODEOWNERS is not
  changed. No teacher approval is claimed.
- The billing exception above is not extended to review.
- No auto-merge, tag, deployment or HU closure is performed.
- Not claimed: an actual API `main` merge, a persistent deployment, and inspection
  of target-environment owners, default privileges or duplicate data (disposable
  tests do not provide these; target creator/default-privileges/duplicate
  preflight evidence remains to be supplied).
- No tag or version is selected. HU317 stays open; central
  reconciliation/scenario 10 stays pending.
- Excluded: HU-DOCS-98 is untouched; TD012 remains open and excluded (no plan).
  TD018 stays partial: five residual differences remain open with
  SergioAndres17 and no review date.
