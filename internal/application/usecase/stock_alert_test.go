package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/in"
	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

type alertHarness struct {
	products *fakeProducts
	alerts   *fakeStockAlerts
	now      time.Time
	svc      *StockAlertService
}

func newAlertHarness(ps ...model.Product) *alertHarness {
	products := newFakeProducts(ps...)
	h := &alertHarness{products: products, alerts: newFakeStockAlerts(products), now: time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)}
	h.svc = NewStockAlertService(h.alerts, sequentialIDs("alert"), func() time.Time { return h.now })
	return h
}

func openCmd(key, productID string, stock int) in.OpenStockAlertCommand {
	return in.OpenStockAlertCommand{IdempotencyKey: key, ProductID: productID, StockAtOpening: stock}
}

// ─── OpenStockAlert ────────────────────────────────────────────────────

func TestOpenStockAlert_OpensAnAlertForTheProduct(t *testing.T) {
	h := newAlertHarness(product("p1", 3, 100))

	res, err := h.svc.OpenStockAlert(context.Background(), openCmd("low-stock:p1:2026-10-09", "p1", 3))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	a := res.Alert
	if !res.Created || a.ProductID != "p1" || a.StockAtOpening != 3 || a.Status != model.AlertStatusOpen || a.ResolvedAt != nil {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestOpenStockAlert_RejectsNegativeStockWithoutCallingTheStore(t *testing.T) {
	h := newAlertHarness(product("p1", 3, 100))

	_, err := h.svc.OpenStockAlert(context.Background(), openCmd("low-stock:p1:2026-10-09", "p1", -1))

	if !errors.Is(err, model.ErrStockAtOpeningNegative) {
		t.Errorf("expected ErrStockAtOpeningNegative, got %v", err)
	}
	if h.alerts.openCalls != 0 {
		t.Errorf("the store was called %d times for an invalid alert", h.alerts.openCalls)
	}
}

func TestOpenStockAlert_ReportsAnUnknownProductAsNotFound(t *testing.T) {
	h := newAlertHarness()

	_, err := h.svc.OpenStockAlert(context.Background(), openCmd("low-stock:p1:2026-10-09", "ghost", 1))

	if !errors.Is(err, in.ErrProductNotFound) {
		t.Errorf("expected ErrProductNotFound, got %v", err)
	}
}

// The Idempotency-Key only collapses an exact retransmission.
func TestOpenStockAlert_ASameKeyRetransmissionReturnsTheOriginal(t *testing.T) {
	h := newAlertHarness(product("p1", 3, 100))
	first, _ := h.svc.OpenStockAlert(context.Background(), openCmd("low-stock:p1:2026-10-09", "p1", 3))

	again, err := h.svc.OpenStockAlert(context.Background(), openCmd("low-stock:p1:2026-10-09", "p1", 3))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if again.Created || again.Alert.ID != first.Alert.ID {
		t.Errorf("expected the original alert %s with Created=false, got %+v", first.Alert.ID, again)
	}
}

// The business rule is the one-OPEN-per-product index: the worker runs
// daily, so the key changes every day, and the product must still end up
// with a single OPEN alert.
func TestOpenStockAlert_ADifferentKeyForAProductWithAnOpenAlertReturnsTheOpenOne(t *testing.T) {
	h := newAlertHarness(product("p1", 3, 100))
	first, _ := h.svc.OpenStockAlert(context.Background(), openCmd("low-stock:p1:2026-10-08", "p1", 3))

	next, err := h.svc.OpenStockAlert(context.Background(), openCmd("low-stock:p1:2026-10-09", "p1", 2))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next.Created || next.Alert.ID != first.Alert.ID {
		t.Errorf("expected the open alert %s with Created=false, got %+v", first.Alert.ID, next)
	}
	if next.Alert.StockAtOpening != 3 {
		t.Errorf("the open alert must keep the stock it was opened at (3), got %d", next.Alert.StockAtOpening)
	}
	if n := h.alerts.count("p1", model.AlertStatusOpen); n != 1 {
		t.Errorf("expected 1 OPEN alert, got %d", n)
	}
}

// RESOLVED is final: a new drop opens a new alert, never reopens.
func TestOpenStockAlert_AfterResolutionANewAlertIsOpened(t *testing.T) {
	h := newAlertHarness(product("p1", 3, 100))
	first, _ := h.svc.OpenStockAlert(context.Background(), openCmd("low-stock:p1:2026-10-08", "p1", 3))
	if _, err := h.svc.ResolveStockAlert(context.Background(), first.Alert.ID); err != nil {
		t.Fatal(err)
	}

	next, err := h.svc.OpenStockAlert(context.Background(), openCmd("low-stock:p1:2026-10-12", "p1", 1))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !next.Created || next.Alert.ID == first.Alert.ID || next.Alert.Status != model.AlertStatusOpen {
		t.Errorf("expected a new OPEN alert, got %+v", next)
	}
	old := h.alerts.byID[first.Alert.ID]
	if old.Status != model.AlertStatusResolved {
		t.Errorf("the resolved alert must stay resolved, got %q", old.Status)
	}
}

func TestOpenStockAlert_MapsAKeyReusedForAnotherResource(t *testing.T) {
	h := newAlertHarness(product("p1", 3, 100))
	h.alerts.err = out.ErrIdempotencyKeyReused

	_, err := h.svc.OpenStockAlert(context.Background(), openCmd("some-key-0001", "p1", 3))

	if !errors.Is(err, in.ErrIdempotencyKeyReused) {
		t.Errorf("expected ErrIdempotencyKeyReused, got %v", err)
	}
}

// ─── ResolveStockAlert ─────────────────────────────────────────────────

func TestResolveStockAlert_SetsResolvedWithTheCurrentTime(t *testing.T) {
	h := newAlertHarness(product("p1", 3, 100))
	opened, _ := h.svc.OpenStockAlert(context.Background(), openCmd("low-stock:p1:2026-10-09", "p1", 3))

	a, err := h.svc.ResolveStockAlert(context.Background(), opened.Alert.ID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Status != model.AlertStatusResolved || a.ResolvedAt == nil || !a.ResolvedAt.Equal(h.now) {
		t.Errorf("unexpected alert: %+v", a)
	}
}

func TestResolveStockAlert_ResolvingAgainChangesNothing(t *testing.T) {
	h := newAlertHarness(product("p1", 3, 100))
	opened, _ := h.svc.OpenStockAlert(context.Background(), openCmd("low-stock:p1:2026-10-09", "p1", 3))
	first, _ := h.svc.ResolveStockAlert(context.Background(), opened.Alert.ID)
	h.now = h.now.Add(48 * time.Hour)

	again, err := h.svc.ResolveStockAlert(context.Background(), opened.Alert.ID)

	if err != nil {
		t.Fatalf("resolving a resolved alert must succeed, got %v", err)
	}
	if again.Status != model.AlertStatusResolved || !again.ResolvedAt.Equal(*first.ResolvedAt) {
		t.Errorf("resolvedAt moved: first %v, again %v", first.ResolvedAt, again.ResolvedAt)
	}
}

func TestResolveStockAlert_ReportsAnUnknownAlertAsNotFound(t *testing.T) {
	h := newAlertHarness()

	_, err := h.svc.ResolveStockAlert(context.Background(), "ghost")

	if !errors.Is(err, in.ErrStockAlertNotFound) {
		t.Errorf("expected ErrStockAlertNotFound, got %v", err)
	}
}

// ─── ListStockAlerts ───────────────────────────────────────────────────

func TestListStockAlerts_ReturnsNewestFirstAndFiltersByStatus(t *testing.T) {
	h := newAlertHarness(product("p1", 1, 100), product("p2", 1, 100), product("p3", 1, 100))
	ctx := context.Background()
	a1, _ := h.svc.OpenStockAlert(ctx, openCmd("low-stock:p1:2026-10-09", "p1", 1))
	a2, _ := h.svc.OpenStockAlert(ctx, openCmd("low-stock:p2:2026-10-09", "p2", 1))
	a3, _ := h.svc.OpenStockAlert(ctx, openCmd("low-stock:p3:2026-10-09", "p3", 1))
	if _, err := h.svc.ResolveStockAlert(ctx, a2.Alert.ID); err != nil {
		t.Fatal(err)
	}

	all, err := h.svc.ListStockAlerts(ctx, in.ListStockAlertsQuery{Page: 1, Limit: 10})
	if err != nil || all.Total != 3 || all.Items[0].ID != a3.Alert.ID || all.Items[2].ID != a1.Alert.ID {
		t.Fatalf("unexpected list: %+v err=%v", all, err)
	}

	resolved := model.AlertStatusResolved
	only, err := h.svc.ListStockAlerts(ctx, in.ListStockAlertsQuery{Page: 1, Limit: 10, Status: &resolved})
	if err != nil || only.Total != 1 || only.Items[0].ID != a2.Alert.ID {
		t.Errorf("unexpected filtered list: %+v err=%v", only, err)
	}
}

func TestListStockAlerts_Paginates(t *testing.T) {
	h := newAlertHarness(product("p1", 1, 100), product("p2", 1, 100), product("p3", 1, 100))
	ctx := context.Background()
	for _, p := range []string{"p1", "p2", "p3"} {
		h.svc.OpenStockAlert(ctx, openCmd("low-stock:"+p+":2026-10-09", p, 1))
	}

	res, err := h.svc.ListStockAlerts(ctx, in.ListStockAlertsQuery{Page: 2, Limit: 2})

	if err != nil || res.Total != 3 || len(res.Items) != 1 {
		t.Errorf("unexpected page 2: %+v err=%v", res, err)
	}
}

// ─── Inactive products ─────────────────────────────────────────────────

func inactiveProduct(id string) model.Product {
	p := product(id, 1, 100)
	p.Active = false
	return p
}

// The product exists but no longer takes part in sales or reservations, so
// an alert about its stock has no action to prompt: a business rule
// violation, not a 404.
func TestOpenStockAlert_RejectsAnInactiveProductAsABusinessRuleViolation(t *testing.T) {
	h := newAlertHarness(inactiveProduct("p1"))

	_, err := h.svc.OpenStockAlert(context.Background(), openCmd("low-stock:p1:2026-10-09", "p1", 1))

	if !errors.Is(err, in.ErrProductInactive) {
		t.Fatalf("expected ErrProductInactive, got %v", err)
	}
	if errors.Is(err, in.ErrProductNotFound) {
		t.Errorf("an inactive product exists: it must not be reported as not found")
	}
	if len(h.alerts.byID) != 0 {
		t.Errorf("no alert may be stored, found %d", len(h.alerts.byID))
	}
}

// An exact retransmission is answered from the key, even if the product
// was deactivated after the alert was opened.
func TestOpenStockAlert_ASameKeyRetransmissionStillReturnsTheOriginalAfterTheProductIsDeactivated(t *testing.T) {
	h := newAlertHarness(product("p1", 1, 100))
	first, _ := h.svc.OpenStockAlert(context.Background(), openCmd("low-stock:p1:2026-10-09", "p1", 1))
	h.products.byID["p1"] = inactiveProduct("p1")

	again, err := h.svc.OpenStockAlert(context.Background(), openCmd("low-stock:p1:2026-10-09", "p1", 1))

	if err != nil || again.Created || again.Alert.ID != first.Alert.ID {
		t.Errorf("expected the original alert, got %+v err=%v", again, err)
	}
}

// An alert already OPEN for a product deactivated later is not touched.
func TestResolveStockAlert_StillWorksForAProductDeactivatedAfterwards(t *testing.T) {
	h := newAlertHarness(product("p1", 1, 100))
	opened, _ := h.svc.OpenStockAlert(context.Background(), openCmd("low-stock:p1:2026-10-09", "p1", 1))
	h.products.byID["p1"] = inactiveProduct("p1")

	a, err := h.svc.ResolveStockAlert(context.Background(), opened.Alert.ID)

	if err != nil || a.Status != model.AlertStatusResolved {
		t.Errorf("unexpected: %+v err=%v", a, err)
	}
}

// The worker sends a new key every day. A product deactivated while its
// alert is still OPEN must not turn every later run into a 422: the open
// alert is returned as is. The active check only guards creating a new one.
func TestOpenStockAlert_ADifferentKeyForADeactivatedProductWithAnOpenAlertReturnsTheOpenOne(t *testing.T) {
	h := newAlertHarness(product("p1", 1, 100))
	first, _ := h.svc.OpenStockAlert(context.Background(), openCmd("low-stock:p1:2026-10-08", "p1", 1))
	h.products.byID["p1"] = inactiveProduct("p1")

	next, err := h.svc.OpenStockAlert(context.Background(), openCmd("low-stock:p1:2026-10-09", "p1", 1))

	if err != nil {
		t.Fatalf("expected the open alert, got %v", err)
	}
	if next.Created || next.Alert.ID != first.Alert.ID {
		t.Errorf("expected the open alert %s with Created=false, got %+v", first.Alert.ID, next)
	}
}

// Once that alert is resolved nothing is OPEN any more, so the inactive
// product is refused again.
func TestOpenStockAlert_ADeactivatedProductWhoseAlertWasResolvedIsRefusedAgain(t *testing.T) {
	h := newAlertHarness(product("p1", 1, 100))
	first, _ := h.svc.OpenStockAlert(context.Background(), openCmd("low-stock:p1:2026-10-08", "p1", 1))
	h.products.byID["p1"] = inactiveProduct("p1")
	h.svc.ResolveStockAlert(context.Background(), first.Alert.ID)

	_, err := h.svc.OpenStockAlert(context.Background(), openCmd("low-stock:p1:2026-10-09", "p1", 1))

	if !errors.Is(err, in.ErrProductInactive) {
		t.Errorf("expected ErrProductInactive, got %v", err)
	}
}
