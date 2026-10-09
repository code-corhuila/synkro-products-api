package in

import (
	"context"
	"errors"

	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

var ErrStockAlertNotFound = errors.New("stock alert not found")

type OpenStockAlertCommand struct {
	IdempotencyKey string
	ProductID      string
	StockAtOpening int
}

type OpenStockAlertResult struct {
	Alert model.StockAlert
	// Created is false when nothing was written: an exact retransmission
	// or a product that already had an OPEN alert.
	Created bool
}

type ListStockAlertsQuery struct {
	Page, Limit int
	Status      *string // model.AlertStatusOpen or model.AlertStatusResolved
}

type ListStockAlertsResult struct {
	Items []model.StockAlert
	Total int
}

type StockAlertUseCases interface {
	OpenStockAlert(ctx context.Context, cmd OpenStockAlertCommand) (OpenStockAlertResult, error)
	ResolveStockAlert(ctx context.Context, id string) (model.StockAlert, error)
	ListStockAlerts(ctx context.Context, q ListStockAlertsQuery) (ListStockAlertsResult, error)
}

// ErrProductInactive: the product exists but is inactive, so no alert can be
// opened for it (422 BUSINESS_RULE_VIOLATION, not 404).
var ErrProductInactive = errors.New("product is inactive")
