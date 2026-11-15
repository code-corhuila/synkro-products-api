# Products API — main promotion proposal (HU-PRO-15)

## Purpose

Proposal to promote the Products API store-health delivery of HU-PRO-15
(synkro-docs#317) from `qa` to `main`, requested by the owner after both QA
PR24 deliveries were merged. The approved scope is narrow: TD017 own-store
health, with its fixture consuming the paired DB delivery (TD018).
This is a fresh local verification of a proposal. It is **not** an actual merge
into `main`, **not** a persistent deployment and **not** a tag. Historical
QA/develop execution evidence and trails stay separate and unchanged; only
their current-status links are updated.

## Source and base

| Item | Commit |
| --- | --- |
| Source (actual integrated QA merge of API PR24) | `4f0f15be88156ed86ec6755d679721ea026d71f0` |
| Main base | `8c04934ae5b51417a0b88506cc59734ef7913e64` |
| Previous QA head | `22245634d2f10a5667899a24d5c1511d4f3a0891` |
| Candidate (tested) | `47c8e9dbe8e1e8240d7c7376ff1bcaadcdc6cd5e` |
| Fixture `SOURCE_COMMIT` pin (actual integrated DB QA) | `89cd9a670d3e540c3cc524411ff77c058f1b8960` |
| Branch | `release/products-store-health` |

The API QA merge was actually performed by the owner on 2026-11-14 21:34:11
America/Bogota; the DB QA merge (`89cd9a67…`) on 2026-11-14 21:27:34. No actual
`main` merge SHA exists yet and none is claimed. No merge between permanent
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
`.github` change is made. Added on top of the tested candidate: this report, the fresh verification
JSON and current-status links in the QA report and health design. CODEOWNERS is unchanged.

`test/products-db/SOURCE_COMMIT` remains the actual integrated DB QA commit
`89cd9a670d3e540c3cc524411ff77c058f1b8960`. It is valid actual QA content and
matches both candidates; it is not an actual DB `main` merge SHA.

## Fresh validation (local Linux, on the candidate)

Evidence: `docs/evidence/main-promotion-verification.json`. Run on
`golang:1.26.9-alpine3.23` (Go 1.26.9), 2026-11-14 21:37–21:38 America/Bogota (JSON timestamps are UTC), API commit
`47c8e9dbe8e1e8240d7c7376ff1bcaadcdc6cd5e` with DB fixture
`89cd9a670d3e540c3cc524411ff77c058f1b8960`; passed. This run was executed now
on the main candidate; QA and develop evidence is not claimed as newly executed.

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
remains develop history only; the fresh `main` health HTTP and PostgreSQL
integration tests were executed.

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

- The owner alone merges. Order: approve/review/merge the DB `main` PR first.
  Then confirm/update the API fixture provenance from the actual DB `main`
  integration before the final API review and merge.
- `main` requires current approval by ariel5253 on the delivered head, plus the
  existing conversation/review rules. QA/develop approvals do not transfer and
  CODEOWNERS is not changed. No teacher approval is claimed.
- Not claimed: an actual `main` merge, a persistent deployment, and inspection
  of target-environment owners, default privileges or duplicate data (disposable
  tests do not provide these; target creator/default-privileges/duplicate
  preflight evidence remains to be supplied).
- No tag or version is selected. HU317 stays open; central
  reconciliation/scenario 10 stays pending.
- Excluded: HU-DOCS-98 is untouched; TD012 remains open and excluded (no plan).
  TD018 stays partial: five residual differences remain open with
  SergioAndres17 and no review date.
