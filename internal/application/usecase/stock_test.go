package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/in"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

const adminID = "user-admin"

type stockHarness struct {
	products     *fakeProducts
	adjustments  *fakeStockAdjustments
	reservations *fakeStockReservations
	svc          *StockService
}

func newStockHarness(ps ...model.Product) *stockHarness {
	products := newFakeProducts(ps...)
	adj, res := newFakeStockAdjustments(products), newFakeStockReservations(products)
	return &stockHarness{products, adj, res, NewStockService(adj, res, sequentialIDs("id"))}
}

func (h *stockHarness) stock(id string) int { return h.products.byID[id].Stock }

func product(id string, stock int, priceCents int64) model.Product {
	return model.Product{ID: id, Name: id, PriceCents: priceCents, Stock: stock, CategoryID: "cat", Active: true}
}

func adjust(key, productID string, delta int) in.CreateStockAdjustmentCommand {
	return in.CreateStockAdjustmentCommand{IdempotencyKey: key, ProductID: productID, Delta: delta, Reason: "recount", AdjustedBy: adminID}
}

func reserve(key string, lines ...in.ReservationLineCommand) in.CreateStockReservationCommand {
	return in.CreateStockReservationCommand{IdempotencyKey: key, Lines: lines}
}

func line(productID string, qty int) in.ReservationLineCommand {
	return in.ReservationLineCommand{ProductID: productID, Quantity: qty}
}

// ─── Adjustments ───────────────────────────────────────────────────────

func TestCreateStockAdjustment_AppliesThePositiveOrNegativeDelta(t *testing.T) {
	h := newStockHarness(product("p1", 10, 100))

	res, err := h.svc.CreateStockAdjustment(context.Background(), adjust("key-0001", "p1", 5))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Created || res.CurrentStock != 15 || h.stock("p1") != 15 {
		t.Errorf("restock: created=%v currentStock=%d stored=%d, want true/15/15", res.Created, res.CurrentStock, h.stock("p1"))
	}
	if a := res.Adjustment; a.ID != "id-1" || a.ProductID != "p1" || a.Delta != 5 || a.Reason != "recount" || a.AdjustedBy != adminID {
		t.Errorf("adjustment: %+v", a)
	}

	res, err = h.svc.CreateStockAdjustment(context.Background(), adjust("key-0002", "p1", -3))
	if err != nil || res.CurrentStock != 12 || h.stock("p1") != 12 {
		t.Errorf("loss: currentStock=%d stored=%d err=%v, want 12", res.CurrentStock, h.stock("p1"), err)
	}
}

func TestCreateStockAdjustment_MayTakeStockToExactlyZero(t *testing.T) {
	h := newStockHarness(product("p1", 4, 100))
	res, err := h.svc.CreateStockAdjustment(context.Background(), adjust("key-0001", "p1", -4))
	if err != nil || res.CurrentStock != 0 {
		t.Fatalf("currentStock=%d err=%v, want 0 and no error", res.CurrentStock, err)
	}
}

func TestCreateStockAdjustment_BelowZeroIsABusinessRuleViolationAndChangesNothing(t *testing.T) {
	h := newStockHarness(product("p1", 2, 100))

	_, err := h.svc.CreateStockAdjustment(context.Background(), adjust("key-0001", "p1", -3))
	if !errors.Is(err, in.ErrInsufficientStock) {
		t.Fatalf("expected in.ErrInsufficientStock, got %v", err)
	}
	if h.stock("p1") != 2 {
		t.Errorf("stock changed to %d", h.stock("p1"))
	}
	if len(h.adjustments.byID) != 0 {
		t.Errorf("an adjustment was stored: %v", h.adjustments.byID)
	}
}

func TestCreateStockAdjustment_DomainInvariantsStopItBeforeTheRepository(t *testing.T) {
	h := newStockHarness(product("p1", 5, 100))

	zero := adjust("key-0001", "p1", 0)
	if _, err := h.svc.CreateStockAdjustment(context.Background(), zero); !errors.Is(err, model.ErrDeltaZero) {
		t.Errorf("zero delta: got %v", err)
	}
	blank := adjust("key-0002", "p1", 1)
	blank.Reason = "  "
	if _, err := h.svc.CreateStockAdjustment(context.Background(), blank); !errors.Is(err, model.ErrReasonRequired) {
		t.Errorf("blank reason: got %v", err)
	}
	if h.adjustments.createCalls != 0 {
		t.Errorf("repository reached %d times", h.adjustments.createCalls)
	}
}

func TestCreateStockAdjustment_UnknownProductIsNotFound(t *testing.T) {
	h := newStockHarness()
	_, err := h.svc.CreateStockAdjustment(context.Background(), adjust("key-0001", "ghost", 1))
	if !errors.Is(err, in.ErrProductNotFound) {
		t.Fatalf("expected in.ErrProductNotFound, got %v", err)
	}
}

func TestCreateStockAdjustment_ReplayReturnsTheOriginalAndAppliesNothingAgain(t *testing.T) {
	h := newStockHarness(product("p1", 10, 100))
	first, _ := h.svc.CreateStockAdjustment(context.Background(), adjust("key-0001", "p1", -4))

	// Stock has since dropped so far that -4 would no longer fit: a replay
	// must not re-check, either.
	h.products.byID["p1"] = product("p1", 1, 100)
	again, err := h.svc.CreateStockAdjustment(context.Background(), adjust("key-0001", "p1", -4))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if again.Created {
		t.Error("expected Created=false on a replay")
	}
	if again.Adjustment.ID != first.Adjustment.ID {
		t.Errorf("replay returned adjustment %s, want the original %s", again.Adjustment.ID, first.Adjustment.ID)
	}
	if h.stock("p1") != 1 {
		t.Errorf("a replay changed stock to %d", h.stock("p1"))
	}
	if len(h.adjustments.byID) != 1 {
		t.Errorf("expected 1 adjustment, got %d", len(h.adjustments.byID))
	}
}

// ─── Reservations ──────────────────────────────────────────────────────

func TestCreateStockReservation_DecrementsEveryLineAndFreezesThePrice(t *testing.T) {
	h := newStockHarness(product("p1", 10, 1500), product("p2", 5, 700))

	res, err := h.svc.CreateStockReservation(context.Background(), reserve("key-0001", line("p1", 3), line("p2", 5)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Created || res.Reservation.Status != model.StatusReserved {
		t.Errorf("created=%v status=%s", res.Created, res.Reservation.Status)
	}
	if h.stock("p1") != 7 || h.stock("p2") != 0 {
		t.Errorf("stock p1=%d p2=%d, want 7 and 0", h.stock("p1"), h.stock("p2"))
	}
	lines := res.Reservation.Lines
	if len(lines) != 2 || lines[0].ProductID != "p1" || lines[0].Quantity != 3 || lines[0].UnitPriceCents != 1500 ||
		lines[1].ProductID != "p2" || lines[1].Quantity != 5 || lines[1].UnitPriceCents != 700 {
		t.Errorf("lines: %+v", lines)
	}
	if lines[0].LineID == "" || lines[0].LineID == lines[1].LineID {
		t.Errorf("each line needs its own id: %+v", lines)
	}
}

func TestCreateStockReservation_DomainInvariantsStopItBeforeTheRepository(t *testing.T) {
	h := newStockHarness(product("p1", 10, 100))
	tests := []struct {
		name string
		cmd  in.CreateStockReservationCommand
		want error
	}{
		{"no lines", reserve("key-0001"), model.ErrNoLines},
		{"zero quantity", reserve("key-0002", line("p1", 0)), model.ErrQuantityNotPositive},
		{"duplicate product", reserve("key-0003", line("p1", 1), line("p1", 2)), model.ErrDuplicateProduct},
	}
	for _, tc := range tests {
		if _, err := h.svc.CreateStockReservation(context.Background(), tc.cmd); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}
	if h.reservations.createCalls != 0 {
		t.Errorf("repository reached %d times", h.reservations.createCalls)
	}
}

// The most important behavior of the story: one affordable line and one
// that is not. Neither product may lose stock — not even the affordable
// one — and nothing may be stored.
func TestCreateStockReservation_MixedAvailabilityReservesNothing(t *testing.T) {
	orders := map[string][]in.ReservationLineCommand{
		"affordable first":   {line("ok", 2), line("short", 9)},
		"unaffordable first": {line("short", 9), line("ok", 2)},
	}
	for name, lines := range orders {
		t.Run(name, func(t *testing.T) {
			h := newStockHarness(product("ok", 10, 100), product("short", 3, 200))

			_, err := h.svc.CreateStockReservation(context.Background(), reserve("key-0001", lines...))
			if !errors.Is(err, in.ErrInsufficientStock) {
				t.Fatalf("expected in.ErrInsufficientStock, got %v", err)
			}
			var lineErr *in.ReservationLineError
			if !errors.As(err, &lineErr) || lineErr.ProductID != "short" {
				t.Errorf("the error must name the failing product, got %+v", lineErr)
			} else if lines[lineErr.Index].ProductID != "short" {
				t.Errorf("error index %d does not point at the failing line", lineErr.Index)
			}
			if h.stock("ok") != 10 || h.stock("short") != 3 {
				t.Errorf("stock ok=%d short=%d, want 10 and 3 (nothing reserved)", h.stock("ok"), h.stock("short"))
			}
			if len(h.reservations.byID) != 0 {
				t.Errorf("a reservation was stored: %v", h.reservations.byID)
			}
		})
	}
}

func TestCreateStockReservation_InactiveOrUnknownProductReservesNothing(t *testing.T) {
	inactive := product("off", 10, 100)
	inactive.Active = false
	for name, bad := range map[string]string{"inactive": "off", "unknown": "ghost"} {
		t.Run(name, func(t *testing.T) {
			h := newStockHarness(product("ok", 10, 100), inactive)
			_, err := h.svc.CreateStockReservation(context.Background(), reserve("key-0001", line("ok", 1), line(bad, 1)))
			if !errors.Is(err, in.ErrProductUnavailable) {
				t.Fatalf("expected in.ErrProductUnavailable, got %v", err)
			}
			if h.stock("ok") != 10 || len(h.reservations.byID) != 0 {
				t.Errorf("stock ok=%d, reservations=%d: nothing may be reserved", h.stock("ok"), len(h.reservations.byID))
			}
		})
	}
}

func TestCreateStockReservation_ReplayReturnsTheOriginalAndChangesNoStock(t *testing.T) {
	h := newStockHarness(product("p1", 10, 100))
	first, _ := h.svc.CreateStockReservation(context.Background(), reserve("key-0001", line("p1", 4)))

	again, err := h.svc.CreateStockReservation(context.Background(), reserve("key-0001", line("p1", 4)))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if again.Created || again.Reservation.ID != first.Reservation.ID {
		t.Errorf("created=%v id=%s, want false and %s", again.Created, again.Reservation.ID, first.Reservation.ID)
	}
	if h.stock("p1") != 6 {
		t.Errorf("stock %d after the replay, want 6 (reserved once)", h.stock("p1"))
	}
}

func TestCreateStockReservation_PriceIsTheProductsPriceAtReservationTime(t *testing.T) {
	h := newStockHarness(product("p1", 10, 100))
	first, _ := h.svc.CreateStockReservation(context.Background(), reserve("key-0001", line("p1", 1)))

	p := h.products.byID["p1"]
	p.PriceCents = 250
	h.products.byID["p1"] = p
	second, _ := h.svc.CreateStockReservation(context.Background(), reserve("key-0002", line("p1", 1)))

	if got := first.Reservation.Lines[0].UnitPriceCents; got != 100 {
		t.Errorf("first reservation priced at %d, want 100", got)
	}
	if got := second.Reservation.Lines[0].UnitPriceCents; got != 250 {
		t.Errorf("second reservation priced at %d, want 250 (the price at its own time)", got)
	}
}

func TestGetStockReservation_FrozenPriceSurvivesALaterPriceChange(t *testing.T) {
	h := newStockHarness(product("p1", 10, 100))
	created, _ := h.svc.CreateStockReservation(context.Background(), reserve("key-0001", line("p1", 1)))

	p := h.products.byID["p1"]
	p.PriceCents = 999
	h.products.byID["p1"] = p

	got, err := h.svc.GetStockReservation(context.Background(), created.Reservation.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Lines[0].UnitPriceCents != 100 {
		t.Errorf("frozen price moved to %d", got.Lines[0].UnitPriceCents)
	}
}

func TestGetAndReleaseStockReservation_UnknownIsNotFound(t *testing.T) {
	h := newStockHarness()
	if _, err := h.svc.GetStockReservation(context.Background(), "ghost"); !errors.Is(err, in.ErrReservationNotFound) {
		t.Errorf("get: got %v", err)
	}
	if _, err := h.svc.ReleaseStockReservation(context.Background(), "ghost"); !errors.Is(err, in.ErrReservationNotFound) {
		t.Errorf("release: got %v", err)
	}
}

func TestReleaseStockReservation_RestoresStockExactlyOnce(t *testing.T) {
	h := newStockHarness(product("p1", 10, 100), product("p2", 4, 100))
	created, _ := h.svc.CreateStockReservation(context.Background(), reserve("key-0001", line("p1", 3), line("p2", 4)))

	first, err := h.svc.ReleaseStockReservation(context.Background(), created.Reservation.ID)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if first.Status != model.StatusReleased || first.ReleasedAt == nil {
		t.Errorf("status=%s releasedAt=%v", first.Status, first.ReleasedAt)
	}
	if h.stock("p1") != 10 || h.stock("p2") != 4 {
		t.Fatalf("after release p1=%d p2=%d, want 10 and 4", h.stock("p1"), h.stock("p2"))
	}

	second, err := h.svc.ReleaseStockReservation(context.Background(), created.Reservation.ID)
	if err != nil {
		t.Fatalf("second release: %v", err)
	}
	if second.Status != model.StatusReleased {
		t.Errorf("second release status %s", second.Status)
	}
	if h.stock("p1") != 10 || h.stock("p2") != 4 {
		t.Errorf("a second release restored stock again: p1=%d p2=%d", h.stock("p1"), h.stock("p2"))
	}
}

// A replay is "the same resource": currentStock is the stock this
// adjustment left, not whatever the product holds by then.
func TestCreateStockAdjustment_ReplayReportsTheStockTheOriginalLeft(t *testing.T) {
	h := newStockHarness(product("p1", 10, 100))
	first, _ := h.svc.CreateStockAdjustment(context.Background(), adjust("key-0001", "p1", -4))
	if first.CurrentStock != 6 {
		t.Fatalf("setup: first currentStock %d, want 6", first.CurrentStock)
	}
	if _, err := h.svc.CreateStockAdjustment(context.Background(), adjust("key-0002", "p1", 20)); err != nil {
		t.Fatal(err)
	}

	again, err := h.svc.CreateStockAdjustment(context.Background(), adjust("key-0001", "p1", -4))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if again.Created || again.CurrentStock != 6 {
		t.Errorf("replay: created=%v currentStock=%d, want false and 6 (the product holds %d now)", again.Created, again.CurrentStock, h.stock("p1"))
	}
}
