package in

import (
	"context"

	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

type CreateStockAdjustmentCommand struct {
	IdempotencyKey string
	ProductID      string
	Delta          int
	Reason         string
	AdjustedBy     string // the sub of the token, never from the body
}

type CreateStockAdjustmentResult struct {
	Adjustment   model.StockAdjustment
	CurrentStock int
	Created      bool // false on an idempotent replay
}

type ReservationLineCommand struct {
	ProductID string
	Quantity  int
}

type CreateStockReservationCommand struct {
	IdempotencyKey string
	Lines          []ReservationLineCommand
}

type CreateStockReservationResult struct {
	Reservation model.StockReservation
	Created     bool // false on an idempotent replay
}

type StockUseCases interface {
	CreateStockAdjustment(ctx context.Context, cmd CreateStockAdjustmentCommand) (CreateStockAdjustmentResult, error)
	CreateStockReservation(ctx context.Context, cmd CreateStockReservationCommand) (CreateStockReservationResult, error)
	GetStockReservation(ctx context.Context, id string) (model.StockReservation, error)
	ReleaseStockReservation(ctx context.Context, id string) (model.StockReservation, error)
}
