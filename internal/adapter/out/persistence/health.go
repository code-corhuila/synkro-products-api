package persistence

import "context"

// Compile-only scaffold, no query or availability behavior before RED.
func (*Postgres) Check(context.Context) error { return nil }
