package usecase

import (
	"context"
	"errors"
	"github.com/code-corhuila/synkro-products-api/internal/application/port/in"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
	"testing"
	"time"
)

func TestPriceHistory_UsesLockedPriceAuthorAndClockAndSkipsUnchangedPrice(t *testing.T) {
	p := model.Product{ID: "p1", Name: "Mouse", PriceCents: 100, Stock: 7, CategoryID: activeCat.ID, Active: true}
	ps := newFakeProducts(p)
	svc := newProductService(ps, newFakeCategories(activeCat))
	at := time.Date(2026, 1, 2, 10, 0, 0, 0, time.FixedZone("Bogota", -5*3600))
	svc.now = func() time.Time { return at }
	cmd := in.UpdateProductCommand{Name: p.Name, PriceCents: 200, CategoryID: p.CategoryID, ChangedBy: "inventory-one"}
	if _, err := svc.UpdateProduct(context.Background(), p.ID, cmd); err != nil {
		t.Fatal(err)
	}
	if len(ps.history) != 1 || ps.history[0].OldPriceCents != 100 || ps.history[0].NewPriceCents != 200 || ps.history[0].ChangedBy != "inventory-one" || ps.history[0].ChangedAt.Location() != time.UTC || !ps.history[0].ChangedAt.Equal(at) {
		t.Fatalf("%+v", ps.history)
	}
	if _, err := svc.UpdateProduct(context.Background(), p.ID, cmd); err != nil {
		t.Fatal(err)
	}
	if len(ps.history) != 1 {
		t.Fatal("unchanged price generated history")
	}
	cmd.PriceCents = 300
	cmd.ChangedBy = ""
	if _, err := svc.UpdateProduct(context.Background(), p.ID, cmd); !errors.Is(err, model.ErrChangedByRequired) {
		t.Fatal(err)
	}
	if ps.byID[p.ID].PriceCents != 200 {
		t.Fatal("failed decision changed price")
	}
	result, err := svc.ListPriceHistory(context.Background(), in.ListPriceHistoryQuery{ProductID: p.ID, Page: 1, Limit: 20})
	if err != nil || result.Total != 1 {
		t.Fatal(result, err)
	}
	if _, err := svc.ListPriceHistory(context.Background(), in.ListPriceHistoryQuery{ProductID: "missing", Page: 1, Limit: 20}); !errors.Is(err, in.ErrProductNotFound) {
		t.Fatal(err)
	}
}
