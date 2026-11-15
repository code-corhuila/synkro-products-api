# Product price history

Implements code-corhuila/synkro-docs#273. Canonical models and traceability updates
in synkro-docs are deferred by the project owner.

Every price-changing PUT records the previous persisted price, requested price,
verified human subject and UTC instant. An unchanged price produces no event.
This is Products-owned audit evidence, with no new business aggregate or broker.

Repository and Unit of Work ports isolate storage from application decisions.
The unit locks the product, runs the decision on that current state, updates the
catalog and appends history in one transaction. Deactivation uses the same lock
and changes only active, preserving concurrent prices and stock. Stock is never
written by catalog operations. Failure to insert history rolls everything back.

Alternatives: an asynchronous broker introduces an extra availability boundary;
a database trigger lacks the verified caller identity unless propagated separately.
The dominant criterion is atomic, attributable evidence. Accepted costs are one
short row lock per product and a schema deployment before the API deployment.

The table and privileges are owned by synkro-products-db V021–V024. History has
UUIDv7 identity, product id, per-product sequence, old/new integer prices,
changed_by UUID (no cross-domain FK) and changed_at TIMESTAMPTZ. Sequence is
assigned while the product is locked; timestamps do not determine event order.
The application role can SELECT and INSERT history, but cannot UPDATE or DELETE it.
Owners/superusers remain administrative actors outside this privilege boundary.

GET /api/v1/products/{id}/price-history returns newest sequence first, with the
standard data/meta pagination (page >= 1, limit 1–100; defaults 1/20). Reading
requires a human ADMIN or INVENTORY with products:read. Invalid tokens receive
401, other authenticated callers 403, missing/malformed product ids 404, bad
pagination 400. Inactive products retain readable history for those roles.
Responses expose historyId, productId, sequence, oldPriceCents, newPriceCents,
changedBy and changedAt (RFC3339 UTC). Client author/time fields on PUT are ignored.

History count and page use one repeatable-read database snapshot. Memory uses one
mutex for the same atomic contract. No history route or weaker authentication
fallback is optional in the router. Production composition always supplies the
atomic unit and history reader explicitly.

Tests cover two INVENTORY authors through HTTP and real PostgreSQL, no-op price,
forged identity, invalid signatures, authorization, bounded pages, transaction
rollback, concurrent updates, stock preservation and deactivation. See the
local OpenAPI extension and the executable test suites for the contract.
