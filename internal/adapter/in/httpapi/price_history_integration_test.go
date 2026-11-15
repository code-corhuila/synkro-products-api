//go:build integration

package httpapi

import (
	"context"
	"github.com/code-corhuila/synkro-products-api/internal/adapter/out/persistence"
	"github.com/code-corhuila/synkro-products-api/internal/application/usecase"
	"github.com/google/uuid"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPriceHistoryHTTPPostgres_TwoInventoryAuthorsAndNoUnauthenticatedWrite(t *testing.T) {
	pool := openHealthPool(t)
	db := persistence.NewPostgres(pool)
	id := func() string { return uuid.Must(uuid.NewV7()).String() }
	verifier, err := NewRS256Verifier(publicKeyPEM(testKey), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(verifier,
		usecase.NewProductService(db.Products(), db.Categories(), id, db.ProductUpdates(), db.PriceHistory(), time.Now),
		usecase.NewCategoryService(db.Categories(), id),
		usecase.NewStockService(db.StockAdjustments(), db.StockReservations(), id),
		usecase.NewStockAlertService(db.StockAlerts(), id, time.Now), usecase.NewHealthService(db, time.Second))
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	api := &testAPI{t: t, srv: srv}
	productID := exercisePriceHistoryHTTP(t, api)
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM products_schema.product_price_history WHERE product_id=$1 AND changed_by IN ('0192a000-0000-7000-8000-0000000000b2','0192a000-0000-7000-8000-0000000000c3')`, productID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("persisted authors count %d %v", count, err)
	}
}
