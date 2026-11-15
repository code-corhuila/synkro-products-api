package usecase

import (
	"context"
	"time"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
)

type HealthService struct {
	store   out.StoreHealth
	timeout time.Duration
}

func NewHealthService(store out.StoreHealth, timeout time.Duration) *HealthService {
	if store == nil || timeout <= 0 {
		panic("health requires a store and a positive timeout")
	}
	return &HealthService{store: store, timeout: timeout}
}

func (s *HealthService) Healthy(parent context.Context) bool {
	if parent.Err() != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()
	return s.store.Check(ctx) == nil && ctx.Err() == nil
}
