# README pilot review

HU-DOCS-99: code-corhuila/synkro-docs#320. Owner: SantiagoPlazas2005.
Branch: `chore/clean-up-readme`, based on develop `ee50bb7`.
The project user authorized publication of this pilot with SantiagoPlazas2005
as commit author and AsolanoT as PR author, and subsequently authorized rollout
to the other 17 repositories through their own PRs. AUTH-19 working copies
remain unchanged.

## Inventory

| File | Purpose |
|---|---|
| `README.md` | English entry point with quick start, architecture, interface and contribution guidance |
| `docs/authentication.md` | Original authentication detail, preserved from the README |
| `docs/testing.md` | Original test setup, commands, fixture maintenance and coverage detail |
| `docs/readme-pilot-review.md` | Verification scope and rollout handoff |

The original health explanation remains in the README and still links to
`store-health-design.md`. No code, migrations, Compose, locks, CI or CODEOWNERS
were changed. The Spanish reference was used as a layout idea, not copied as a
source of current implementation claims.

## Verification

- Read `go.mod`, `.env.example`, Dockerfile, Compose, router, stock authorization,
  coverage/CI configuration and the migration fixture scripts at the selected source.
- Correctly distinguish `stock:reserve` from `stock:release`; preserve the updated
  store-sensitive 200/503 health behavior and real integration coverage gates.
- Local document targets exist. The diagram has seven nodes and quoted labels;
  its shared-client, gateway, service and database relationships match source.
- Preserve every old README section anchor: `running-locally` (explicit anchor),
  `authentication`, `running-the-tests` and `branching`.
- A Markdown scan of the original sibling workspace found no `README.md#...`
  references to update. External synkro-docs anchors were not independently
  enumerated; retaining all previous section anchors avoids breaking those links.
- Unit/build/database commands were inspected against executable source and CI.
  They were not rerun for this documentation-only pilot. No new runtime result is
  inferred from a source inspection or a historical report.
- PowerShell execution commands assume the documented environment variables have
  already been supplied. The README explicitly says `.env` is not automatically
  loaded and distinguishes host access from internal Docker addresses.

## Rollout

The project user authorized extension of the format to the remaining repositories.
Each owning PR inventories and preserves its detailed procedures before replacing
links. Auth follows the merged AUTH-19 delivery, and infrastructure incorporates
HU-INF-08's delivered development-deployment command. Each repository enters
develop through its own documentation PR and retains its actual review status.
