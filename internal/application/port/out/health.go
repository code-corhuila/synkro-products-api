package out

import "context"

type StoreHealth interface { Check(context.Context) error }
