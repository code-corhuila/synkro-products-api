# Products API — QA promotion proposal (HU-PRO-15)

Current status: both QA PR24 integrations are complete. DB: `89cd9a670d3e540c3cc524411ff77c058f1b8960`; API: `4f0f15be88156ed86ec6755d679721ea026d71f0`.
The current [main proposal](main-promotion.md) remains pending review/integration.
The candidate executions and trails below remain historical QA evidence.

## Purpose

Proposal to promote the Products API store-health delivery of HU-PRO-15
(synkro-docs#317) from `develop` to `qa`. This repository delivers TD017; its
fixture consumes the two approved TD018 corrections from the paired DB proposal. This is a fresh local verification of a proposal. It is **not**
a persistent QA deployment and **not** an executed promotion to `main`.
Historical develop evidence in `docs/evidence/` is kept separate and unchanged.

## Source and base

| Item | Commit |
| --- | --- |
| Source (develop) | `ee50bb7c6c1d2b88ce9e83eec17751961f4883cf` |
| QA base | `22245634d2f10a5667899a24d5c1511d4f3a0891` |
| Candidate (tested) | `e46ac534638e34ec56be55ab14107ce93f2599ac` |
| Branch | `qa-products-store-health` |
| Integrated DB QA (current fixture `SOURCE_COMMIT` pin) | `89cd9a670d3e540c3cc524411ff77c058f1b8960` |
| Previous pin (DB develop; historical, used by the tested candidate run) | `0867b1e98e65ed3c24a170248961ef04d1a44a89` |

DB QA PR24 ([synkro-products-db#24](https://github.com/code-corhuila/synkro-products-db/pull/24))
was actually merged into DB QA as `89cd9a670d3e540c3cc524411ff77c058f1b8960`
on 2026-11-14 21:27:34 America/Bogota. API QA PR24 was integrated as `4f0f15be88156ed86ec6755d679721ea026d71f0`
on 2026-11-14 at 21:34:11 America/Bogota.
Evidence of the DB integration (read-only, produced by Codex):
[`docs/evidence/qa-db-integration.json`](evidence/qa-db-integration.json).

The QA child uses the repository's existing `qa-...` naming: GitHub rejected
`qa/...` with a directory/file conflict against the permanent `qa` ref.
No merge between permanent branches was made. All four feature commits were
applied with `cherry-pick -x`.

## Source → QA trail

Each QA commit message carries the literal line shown.

1. `7a516aeb7b247c930a9270da9bac46d41ab9fa6b` →
   `17e271c8f5b20596d83ccafb2adb879e69da009f`
   (`test(health): specify bounded store probe before implementation`)

   `(cherry picked from commit 7a516aeb7b247c930a9270da9bac46d41ab9fa6b)`

2. `c453a8e36a717b6635c4942ab0d4a8b93d363731` →
   `aceb430c47a70a647f93efe7ae5664b56a42455f`
   (`feat(health): report bounded Products store availability`)

   `(cherry picked from commit c453a8e36a717b6635c4942ab0d4a8b93d363731)`

3. `55d26f8ac65d21137f2e170fa3655f33a06185bd` →
   `936ed4480694dc37aaacef7820bedeebfdd6e043`
   (`docs(health): record complete Linux validation and delivery gates`)

   `(cherry picked from commit 55d26f8ac65d21137f2e170fa3655f33a06185bd)`

4. `3e33d457a1c666608415f006f309b428e2b4bf3a` →
   `e46ac534638e34ec56be55ab14107ce93f2599ac`
   (`chore(test): pin merged Products DB revision`)

   `(cherry picked from commit 3e33d457a1c666608415f006f309b428e2b4bf3a)`

## Candidate and scope

The candidate tree is identical to the tree of develop source
`ee50bb7c6c1d2b88ce9e83eec17751961f4883cf`. Runtime code, tests and
configuration match develop exactly; the only changes added on top of the tested
candidate are documentation and provenance (this report, the verification and
DB-integration JSON files, the final status sentence of
`docs/store-health-design.md`, and the `test/products-db/SOURCE_COMMIT` pin
moved from `0867b1e9…` to `89cd9a67…`). Production SQL/Go, tests and runtime
configuration are identical to the candidate; no new test run was made for the
pin update. CODEOWNERS is unchanged. TD012 (open) is excluded.
HU-DOCS-98 is untouched.

DB QA integration (see `docs/evidence/qa-db-integration.json`): the DB QA merge
tree equals the published DB head
`fb1192226831e55ca3ae03166c65126fa34b6efa`; all 38 vendored API
migration/config/placeholder blobs match the real DB QA merge; and the DB
runtime content equals the tested DB candidate
`ce8fd682e143049977302b1666e8e69bf3dedafa` (only two QA docs differ). The
preserved validation below therefore still applies to identical executable
content.

## Fresh validation (local Linux, on the candidate)

Historical verification: it was executed on API candidate
`e46ac534638e34ec56be55ab14107ce93f2599ac` with the then-current fixture pin
`0867b1e98e65ed3c24a170248961ef04d1a44a89`. It is not re-run for pin
`89cd9a67…`; that pin is provenance only (see above).

Evidence: `docs/evidence/qa-promotion-verification.json`. Run on
`golang:1.26.9-alpine3.23` (Go 1.26.9), 2026-11-14 America/Bogota (JSON timestamps are UTC), API commit
`e46ac534638e34ec56be55ab14107ce93f2599ac`; passed.

- Stages: common files, build, vet (including integration), unit, domain architecture, integration.
- Migrations: 20 versions applied on real PostgreSQL.
- Unit: 219 top-level tests; 350 passing test/subtest events; 56 skipped events
  (legacy DB-dependent tests, skipped in the unit stage only); no failures.
- Integration (real PostgreSQL): 282 top-level tests; 421 passing events;
  0 skips; no failures.
- Coverage (all gates pass): domain 100% (min 90), application 90.4% (min 80),
  infrastructure 91.42% (min 60), global 89.9% (min 80).
- Format: all 70 committed Go source files (LF) pass `gofmt`.
- Fixture: all 38 vendored DB migration/config/placeholder blobs match the DB QA
  candidate `ce8fd682e143049977302b1666e8e69bf3dedafa`; `SOURCE_COMMIT` points
  to the then-integrated DB develop commit
  `0867b1e98e65ed3c24a170248961ef04d1a44a89` (historical pin of this run; the
  current pin is DB QA `89cd9a67…`, with the same 38 blobs).
- No owned containers or networks remain.

No new process-outage run was made for QA. The earlier 15-check process run
(`docs/evidence/process-outage-and-reservations.json`) remains historical
develop evidence only. The Linux QA integration stage exercises the current
actual HTTP and store behavior.

## Pact scope

The [testing strategy, Tier 3b](https://github.com/code-corhuila/synkro-docs/blob/main/11-quality/testing-strategy.md#tier-3b-pact)
and [branching policy](https://github.com/code-corhuila/synkro-docs/blob/main/00-governance/branching-policy.md) distinguish consumer-driven Pact from
OpenAPI/HTTP tests.

- This change introduces no outgoing HTTP client or call, and changes no
  existing Products business-route contract or consumer serialization. Public
  `/health` implements the already documented 200/503 `HealthResponse`
  (OpenAPI source file blob SHA
  `d99ae3f57d694e8b68260c80924e01e3edf60c43`).
- The bounded Pact matrix currently covers Customers/Sales only; no Products
  Pact fixture was found. Under the unchanged-HTTP-call rule no new or
  regenerated consumer Pact is required here.
- No Pact was executed and no Pact result, waiver or green status is claimed.
  The executed evidence is the actual health HTTP tests and the real-database
  tests above.

## Billing exception

The owner decided on 2026-11-14 (America/Bogota) that the GitHub Actions
account-billing failure is not an impediment for this delivery only. Jobs that
never started are not successful tests, and no green remote CI is claimed.
Review, integrity checks, tests and branch rules still apply.

## Order, merge and limits

- Both actual QA integrations are complete. Main proposals are tracked in
  [main-promotion.md](main-promotion.md), using cherry-pick -x from actual QA.
- The owner alone merges. Main order: DB first, then API after confirmation of
  its fixture provenance against the actual DB main integration.
- Main requires current ariel5253 approval on the delivered head and the
  existing review/conversation rules. Earlier approvals do not transfer.
- Not claimed: a persistent QA deployment, and inspection of target-environment
  owners, default privileges or duplicate data (disposable tests do not show
  these).
- No tag, rollout, or closure of HU-PRO-15 / synkro-docs#317. TD018 stays
  partial: five residual items remain open, coordinated by SergioAndres17,
  with no review date.
