package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/code-corhuila/synkro-products-api/internal/adapter/in/httpapi"
	"github.com/code-corhuila/synkro-products-api/internal/adapter/out/persistence"
	"github.com/code-corhuila/synkro-products-api/internal/application/usecase"
	"github.com/code-corhuila/synkro-products-api/internal/config"
)

// newID makes UUIDv7 ids: time-ordered, so "newest first" can be an
// ORDER BY id DESC (products_schema has no created_at column).
func newID() string { return uuid.Must(uuid.NewV7()).String() }

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err == nil {
		err = pool.Ping(ctx)
	}
	cancel()
	if err != nil {
		logger.Error("cannot reach the database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	db := persistence.NewPostgres(pool)
	router := httpapi.NewRouter(
		usecase.NewProductService(db.Products(), db.Categories(), newID),
		usecase.NewCategoryService(db.Categories(), newID),
		usecase.NewStockService(db.StockAdjustments(), db.StockReservations(), newID),
	)

	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	logger.Info("starting synkro-products-api", "port", cfg.HTTPPort)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
