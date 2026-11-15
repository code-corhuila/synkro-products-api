package in

import (
	"context"

	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

type ListPriceHistoryQuery struct {
	ProductID   string
	Page, Limit int
}

type ListPriceHistoryResult struct {
	Items []model.PriceChange
	Total int
}

type PriceHistoryUseCases interface {
	// ListPriceHistory: ErrProductNotFound if the product does not exist.
	ListPriceHistory(ctx context.Context, q ListPriceHistoryQuery) (ListPriceHistoryResult, error)
}
