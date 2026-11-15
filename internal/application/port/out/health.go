package out

import "context"

// StoreHealth checks this service's store and must honor context cancellation.
type StoreHealth interface{ Check(context.Context) error }
