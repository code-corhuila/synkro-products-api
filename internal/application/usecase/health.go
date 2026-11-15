package usecase

import (
    "context"
    "time"
    "github.com/code-corhuila/synkro-products-api/internal/application/port/out"
)

// Compile-only scaffold preserves the old unconditional liveness for RED.
type HealthService struct{}
func NewHealthService(out.StoreHealth, time.Duration) *HealthService { return &HealthService{} }
func (*HealthService) Healthy(context.Context) bool { return true }
