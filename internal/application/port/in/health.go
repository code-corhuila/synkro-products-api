package in

import "context"

// HealthUseCases exposes availability without exposing infrastructure errors.
type HealthUseCases interface{ Healthy(context.Context) bool }
