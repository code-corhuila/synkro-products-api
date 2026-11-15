package in

import "context"

type HealthUseCases interface { Healthy(context.Context) bool }
