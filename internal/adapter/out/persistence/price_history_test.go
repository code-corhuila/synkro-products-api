package persistence

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

func incrementPrice(id string) out.ProductUpdateDecision {
	return func(p model.Product) (model.Product, *model.PriceChange, error) {
		next := p
		next.PriceCents++
		h, err := model.NewPriceChange(id, p.ID, p.PriceCents, next.PriceCents, "77777777-7777-7777-7777-777777777777", time.Now())
		return next, &h, err
	}
}

func exercisePriceHistory(t *testing.T, unit out.ProductUpdateUnit, history out.PriceHistoryRepository, p model.Product) {
	t.Helper()
	ctx := context.Background()
	before, err := unit.Apply(ctx, p.ID, incrementPrice(newID()))
	if err != nil || before.PriceCents != p.PriceCents+1 || before.Stock != p.Stock {
		t.Fatalf("update %+v %v", before, err)
	}
	items, total, err := history.ListByProduct(ctx, p.ID, 1, 100)
	if err != nil || total != 1 || len(items) != 1 || items[0].OldPriceCents != p.PriceCents || items[0].Sequence != 1 {
		t.Fatalf("history %+v %d %v", items, total, err)
	}
	// A duplicate history identifier makes the insert fail after catalog UPDATE.
	if _, err := unit.Apply(ctx, p.ID, incrementPrice(items[0].ID)); err == nil {
		t.Fatal("duplicate history id accepted")
	}
	unchanged, err := unit.Apply(ctx, p.ID, func(current model.Product) (model.Product, *model.PriceChange, error) { return current, nil, nil })
	if err != nil || unchanged != before {
		t.Fatalf("rollback failed %+v %v", unchanged, err)
	}
	if _, n, _ := history.ListByProduct(ctx, p.ID, 1, 100); n != 1 {
		t.Fatal("rollback added history")
	}
	sentinel := errors.New("abandon decision")
	if _, err := unit.Apply(ctx, p.ID, func(current model.Product) (model.Product, *model.PriceChange, error) { return current, nil, sentinel }); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if _, err := unit.Apply(ctx, "00000000-0000-0000-0000-000000000001", incrementPrice(newID())); !errors.Is(err, out.ErrNotFound) {
		t.Fatal(err)
	}
	if _, _, err := history.ListByProduct(ctx, "00000000-0000-0000-0000-000000000001", 1, 20); !errors.Is(err, out.ErrNotFound) {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := unit.Apply(ctx, p.ID, incrementPrice(newID())); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	items, total, err = history.ListByProduct(ctx, p.ID, 1, 100)
	if err != nil || total != 9 {
		t.Fatalf("concurrent total %d %v", total, err)
	}
	for i, h := range items {
		if h.Sequence != int64(9-i) || h.NewPriceCents != h.OldPriceCents+1 {
			t.Fatalf("broken chain %+v", items)
		}
		if i > 0 && h.NewPriceCents != items[i-1].OldPriceCents {
			t.Fatalf("noncontiguous chain %+v", items)
		}
	}
	if page, n, err := history.ListByProduct(ctx, p.ID, 2, 2); err != nil || n != 9 || len(page) != 2 || page[0].Sequence != 7 {
		t.Fatalf("page %+v %d %v", page, n, err)
	}
	if page, n, err := history.ListByProduct(ctx, p.ID, 99, 2); err != nil || n != 9 || len(page) != 0 {
		t.Fatalf("past end %+v %d %v", page, n, err)
	}
	// Deactivation based on the locked state cannot restore an earlier price.
	final, err := unit.Apply(ctx, p.ID, func(current model.Product) (model.Product, *model.PriceChange, error) {
		current.Deactivate()
		return current, nil, nil
	})
	if err != nil || final.PriceCents != p.PriceCents+9 || final.Stock != p.Stock || final.Active {
		t.Fatalf("deactivate %+v %v", final, err)
	}
}

func TestMemoryPriceHistory_AtomicRollbackAndConcurrentOrder(t *testing.T) {
	m := NewMemory()
	p := model.Product{ID: newID(), Name: "Mouse", PriceCents: 100, Stock: 7, CategoryID: newID(), Active: true}
	m.SeedProduct(p)
	exercisePriceHistory(t, m.ProductUpdates(), m.PriceHistory(), p)
}

func TestPostgresPriceHistory_AtomicRollbackAndConcurrentOrder(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	cat := mustCategory(t, db.Categories(), "History "+uniq())
	p := mustProduct(t, db.Products(), "History "+uniq(), cat.ID)
	setStock(t, pool, p.ID, 7)
	p.Stock = 7
	exercisePriceHistory(t, db.ProductUpdates(), db.PriceHistory(), p)
}
