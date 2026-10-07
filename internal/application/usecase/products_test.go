package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/in"
	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

var (
	activeCat   = model.Category{ID: "cat-active", Name: "Peripherals", Active: true}
	inactiveCat = model.Category{ID: "cat-inactive", Name: "Old Stuff", Active: false}
)

func newProductService(ps *fakeProducts, cs *fakeCategories) *ProductService {
	return NewProductService(ps, cs, sequentialIDs("prod"))
}

func validCreate(key string) in.CreateProductCommand {
	return in.CreateProductCommand{IdempotencyKey: key, Name: "Mouse", PriceCents: 4599_00, CategoryID: activeCat.ID}
}

func TestCreateProduct_CreatesWithStockZeroInAnActiveCategory(t *testing.T) {
	ps := newFakeProducts()
	svc := newProductService(ps, newFakeCategories(activeCat))

	res, err := svc.CreateProduct(context.Background(), validCreate("key-0001"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Created {
		t.Error("expected Created=true on the first request")
	}
	want := model.Product{ID: "prod-1", Name: "Mouse", PriceCents: 4599_00, Stock: 0, CategoryID: activeCat.ID, Active: true}
	if res.Product != want {
		t.Errorf("got %+v, want %+v", res.Product, want)
	}
	if ps.byID["prod-1"] != want {
		t.Errorf("not persisted as expected: %+v", ps.byID["prod-1"])
	}
}

func TestCreateProduct_ReplayReturnsTheOriginalAndCreatesNothing(t *testing.T) {
	ps := newFakeProducts()
	svc := newProductService(ps, newFakeCategories(activeCat))

	first, _ := svc.CreateProduct(context.Background(), validCreate("key-0001"))
	again, err := svc.CreateProduct(context.Background(), validCreate("key-0001"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if again.Created {
		t.Error("expected Created=false on a replay")
	}
	if again.Product != first.Product {
		t.Errorf("replay returned %+v, want the original %+v", again.Product, first.Product)
	}
	if len(ps.byID) != 1 {
		t.Errorf("expected exactly 1 product stored, got %d", len(ps.byID))
	}
}

// 404, not 422: the contract says a bad category on create is "not found".
func TestCreateProduct_UnknownCategoryIsNotFoundNotABusinessRuleViolation(t *testing.T) {
	ps := newFakeProducts()
	svc := newProductService(ps, newFakeCategories(activeCat))

	cmd := validCreate("key-0001")
	cmd.CategoryID = "cat-missing"
	_, err := svc.CreateProduct(context.Background(), cmd)
	if !errors.Is(err, in.ErrCategoryNotFound) {
		t.Fatalf("expected ErrCategoryNotFound, got %v", err)
	}
	if errors.Is(err, in.ErrCategoryNameTaken) || errors.Is(err, in.ErrCategoryHasActiveProducts) {
		t.Fatal("a bad category on create must not be reported as a business-rule violation")
	}
	if len(ps.byID) != 0 {
		t.Error("nothing should have been stored")
	}
}

func TestCreateProduct_InactiveCategoryIsNotFound(t *testing.T) {
	ps := newFakeProducts()
	svc := newProductService(ps, newFakeCategories(inactiveCat))

	cmd := validCreate("key-0001")
	cmd.CategoryID = inactiveCat.ID
	_, err := svc.CreateProduct(context.Background(), cmd)
	if !errors.Is(err, in.ErrCategoryNotFound) {
		t.Fatalf("expected ErrCategoryNotFound, got %v", err)
	}
	if len(ps.byID) != 0 {
		t.Error("nothing should have been stored")
	}
}

func TestCreateProduct_DomainInvariantsAreApplied(t *testing.T) {
	svc := newProductService(newFakeProducts(), newFakeCategories(activeCat))

	cmd := validCreate("key-0001")
	cmd.Name = ""
	if _, err := svc.CreateProduct(context.Background(), cmd); !errors.Is(err, model.ErrNameRequired) {
		t.Errorf("expected ErrNameRequired, got %v", err)
	}
	cmd = validCreate("key-0002")
	cmd.PriceCents = 0
	if _, err := svc.CreateProduct(context.Background(), cmd); !errors.Is(err, model.ErrPriceNotPositive) {
		t.Errorf("expected ErrPriceNotPositive, got %v", err)
	}
}

func TestCreateProduct_KeyReusedForAnotherResourceIsReported(t *testing.T) {
	ps := newFakeProducts()
	ps.err = out.ErrIdempotencyKeyReused
	svc := newProductService(ps, newFakeCategories(activeCat))

	_, err := svc.CreateProduct(context.Background(), validCreate("key-0001"))
	if !errors.Is(err, in.ErrIdempotencyKeyReused) {
		t.Fatalf("expected ErrIdempotencyKeyReused, got %v", err)
	}
}

func TestListProducts_PassesTheFilterThrough(t *testing.T) {
	ps := newFakeProducts(model.Product{ID: "p1", Name: "Mouse", PriceCents: 1, Active: true})
	svc := newProductService(ps, newFakeCategories())

	cat, active, name, atMost := "c1", true, "mou", 5
	res, err := svc.ListProducts(context.Background(), in.ListProductsQuery{
		Page: 2, Limit: 10, CategoryID: &cat, Active: &active, Name: &name, StockAtMost: &atMost,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := ps.listed
	if got.Page != 2 || got.Limit != 10 || *got.CategoryID != "c1" || !*got.Active || *got.Name != "mou" || *got.StockAtMost != 5 {
		t.Errorf("filter not passed through: %+v", got)
	}
	if res.Total != 1 || len(res.Items) != 1 {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestGetProduct_MissingIsNotFound(t *testing.T) {
	svc := newProductService(newFakeProducts(), newFakeCategories())
	_, err := svc.GetProduct(context.Background(), "nope")
	if !errors.Is(err, in.ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}
}

func TestGetProduct_ReturnsTheProduct(t *testing.T) {
	p := model.Product{ID: "p1", Name: "Mouse", PriceCents: 100, Stock: 3, CategoryID: "c", Active: true}
	svc := newProductService(newFakeProducts(p), newFakeCategories())
	res, err := svc.GetProduct(context.Background(), "p1")
	if err != nil || res.Product != p {
		t.Fatalf("got %+v, %v", res.Product, err)
	}
}

func TestUpdateProduct_ChangesDetailsAndKeepsStock(t *testing.T) {
	other := model.Category{ID: "cat-other", Name: "Audio", Active: true}
	p := model.Product{ID: "p1", Name: "Mouse", PriceCents: 100, Stock: 9, CategoryID: activeCat.ID, Active: true}
	ps := newFakeProducts(p)
	svc := newProductService(ps, newFakeCategories(activeCat, other))

	res, err := svc.UpdateProduct(context.Background(), "p1", in.UpdateProductCommand{Name: "Headset", PriceCents: 200, CategoryID: other.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := model.Product{ID: "p1", Name: "Headset", PriceCents: 200, Stock: 9, CategoryID: other.ID, Active: true}
	if res.Product != want || ps.byID["p1"] != want {
		t.Errorf("got %+v / stored %+v, want %+v", res.Product, ps.byID["p1"], want)
	}
}

func TestUpdateProduct_MissingProductIsNotFound(t *testing.T) {
	svc := newProductService(newFakeProducts(), newFakeCategories(activeCat))
	_, err := svc.UpdateProduct(context.Background(), "nope", in.UpdateProductCommand{Name: "X", PriceCents: 1, CategoryID: activeCat.ID})
	if !errors.Is(err, in.ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}
}

// Same 404 rule as create, re-checked on update.
func TestUpdateProduct_InactiveOrUnknownCategoryIsNotFound(t *testing.T) {
	p := model.Product{ID: "p1", Name: "Mouse", PriceCents: 100, CategoryID: activeCat.ID, Active: true}
	for _, catID := range []string{inactiveCat.ID, "cat-missing"} {
		ps := newFakeProducts(p)
		svc := newProductService(ps, newFakeCategories(activeCat, inactiveCat))
		_, err := svc.UpdateProduct(context.Background(), "p1", in.UpdateProductCommand{Name: "Mouse", PriceCents: 100, CategoryID: catID})
		if !errors.Is(err, in.ErrCategoryNotFound) {
			t.Errorf("category %s: expected ErrCategoryNotFound, got %v", catID, err)
		}
		if ps.updates != 0 || ps.byID["p1"] != p {
			t.Errorf("category %s: the product must not change", catID)
		}
	}
}

func TestUpdateProduct_DomainInvariantsAreApplied(t *testing.T) {
	p := model.Product{ID: "p1", Name: "Mouse", PriceCents: 100, CategoryID: activeCat.ID, Active: true}
	ps := newFakeProducts(p)
	svc := newProductService(ps, newFakeCategories(activeCat))
	_, err := svc.UpdateProduct(context.Background(), "p1", in.UpdateProductCommand{Name: "Mouse", PriceCents: -5, CategoryID: activeCat.ID})
	if !errors.Is(err, model.ErrPriceNotPositive) {
		t.Fatalf("expected ErrPriceNotPositive, got %v", err)
	}
	if ps.updates != 0 {
		t.Error("nothing should have been persisted")
	}
}

func TestDeactivateProduct_SetsInactive(t *testing.T) {
	p := model.Product{ID: "p1", Name: "Mouse", PriceCents: 100, Stock: 2, CategoryID: "c", Active: true}
	ps := newFakeProducts(p)
	svc := newProductService(ps, newFakeCategories())

	res, err := svc.DeactivateProduct(context.Background(), "p1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Product.Active || ps.byID["p1"].Active {
		t.Error("expected the product to be inactive")
	}
	if res.Product.Stock != 2 {
		t.Error("deactivation must not touch stock")
	}
}

func TestDeactivateProduct_AlreadyInactiveSucceedsWithoutWriting(t *testing.T) {
	p := model.Product{ID: "p1", Name: "Mouse", PriceCents: 100, CategoryID: "c", Active: false}
	ps := newFakeProducts(p)
	svc := newProductService(ps, newFakeCategories())

	res, err := svc.DeactivateProduct(context.Background(), "p1")
	if err != nil {
		t.Fatalf("deactivating an inactive product must succeed, got %v", err)
	}
	if res.Product != p {
		t.Errorf("expected the current state %+v, got %+v", p, res.Product)
	}
	if ps.updates != 0 {
		t.Errorf("expected no write, got %d", ps.updates)
	}
}

func TestDeactivateProduct_MissingIsNotFound(t *testing.T) {
	svc := newProductService(newFakeProducts(), newFakeCategories())
	if _, err := svc.DeactivateProduct(context.Background(), "nope"); !errors.Is(err, in.ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}
}

func TestProductService_RepositoryFailuresAreNotDisguisedAsNotFound(t *testing.T) {
	boom := errors.New("connection reset")
	ps := newFakeProducts()
	ps.err = boom
	svc := newProductService(ps, newFakeCategories(activeCat))
	_, err := svc.GetProduct(context.Background(), "p1")
	if !errors.Is(err, boom) || errors.Is(err, in.ErrProductNotFound) {
		t.Fatalf("expected the raw failure, got %v", err)
	}
}
