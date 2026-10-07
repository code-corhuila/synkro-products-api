package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"

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
	cfg := config.Load()

	mem := persistence.NewMemory()
	router := httpapi.NewRouter(
		usecase.NewProductService(mem.Products(), mem.Categories(), newID),
		usecase.NewCategoryService(mem.Categories(), newID),
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
