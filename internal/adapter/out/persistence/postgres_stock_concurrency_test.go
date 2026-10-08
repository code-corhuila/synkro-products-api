package persistence

import (
	"errors"
	"sync"
	"testing"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// Concurrency tests for the stock adapters, against real PostgreSQL. The
// sequential tests prove the rules; these prove the locking: that a
// check-then-write made by one request cannot be invalidated by another
// in between. Each one releases all its goroutines at once from a closed
// channel, and accepts no failure other than the business one — a
// deadlock (40P01) or serialization error would surface as an
// unexpected error.

// race runs fn(i) for i in [0,n) with all goroutines released together.
func race(n int, fn func(i int)) {
	var ready, done sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		ready.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			fn(i)
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
}

// The rule the whole story exists for, under real concurrency: reservations
// that overlap on two products, where one product is the bottleneck. A
// request that fails on the bottleneck must not have taken the other
// product's stock — so, at the end, the other product has lost exactly
// what the successful reservations took, no more. Half of the requests
// list the products in the opposite order, which is what would deadlock
// an adapter that locked them in request order.
func TestPostgres_StockReservation_ConcurrentOverlappingReservationsAreAllOrNothing(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)

	const rounds, requests = 5, 24
	for round := range rounds {
		plenty := stockedProduct(t, db, pool, 100) // never the limit
		scarce := stockedProduct(t, db, pool, 7)   // the bottleneck
		results := make([]error, requests)

		race(requests, func(i int) {
			lines := []model.ReservationLine{want(plenty.ID, 1), want(scarce.ID, 1)}
			if i%2 == 1 {
				lines[0], lines[1] = lines[1], lines[0]
			}
			_, _, results[i] = db.StockReservations().CreateOnce(ctxT(), newKey(), newReservation(t, lines...))
		})

		reserved, refused := 0, 0
		for i, err := range results {
			switch {
			case err == nil:
				reserved++
			case errors.Is(err, out.ErrInsufficientStock):
				refused++
			default:
				t.Fatalf("round %d request %d: unexpected error (a deadlock would show here): %v", round, i, err)
			}
		}
		if reserved != 7 || refused != requests-7 {
			t.Errorf("round %d: reserved=%d refused=%d, want 7 and %d", round, reserved, refused, requests-7)
		}
		if got := stockOf(t, pool, scarce.ID); got != 0 {
			t.Errorf("round %d: scarce stock %d, want 0 (never negative, never oversold)", round, got)
		}
		if got := stockOf(t, pool, plenty.ID); got != 100-reserved {
			t.Errorf("round %d: plenty stock %d, want %d: a refused reservation must not keep the stock it took from the other product", round, got, 100-reserved)
		}
	}
}

func TestPostgres_StockAdjustment_ConcurrentWithdrawalsNeverTakeStockBelowZero(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 10)

	const requests = 25
	results := make([]error, requests)
	race(requests, func(i int) {
		_, _, _, results[i] = db.StockAdjustments().CreateOnce(ctxT(), newKey(), newAdjustment(t, p.ID, -1))
	})

	applied, refused := 0, 0
	for i, err := range results {
		switch {
		case err == nil:
			applied++
		case errors.Is(err, out.ErrInsufficientStock):
			refused++
		default:
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
	}
	if applied != 10 || refused != requests-10 {
		t.Errorf("applied=%d refused=%d, want 10 and %d", applied, refused, requests-10)
	}
	if got := stockOf(t, pool, p.ID); got != 0 {
		t.Errorf("stock %d, want 0", got)
	}
	if n := adjustmentRows(t, pool, p.ID); n != 10 {
		t.Errorf("%d adjustment rows, want 10 (a refused withdrawal must leave none)", n)
	}
}

func TestPostgres_StockAdjustment_ConcurrentRequestsWithOneKeyApplyExactlyOnce(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 10)
	key := newKey()

	const requests = 8
	ids := make([]string, requests)
	created := make([]bool, requests)
	errs := make([]error, requests)
	race(requests, func(i int) {
		var a model.StockAdjustment
		a, _, created[i], errs[i] = db.StockAdjustments().CreateOnce(ctxT(), key, newAdjustment(t, p.ID, -3))
		ids[i] = a.ID
	})

	n := 0
	for i := range requests {
		if errs[i] != nil {
			t.Fatalf("request %d: %v", i, errs[i])
		}
		if created[i] {
			n++
		}
		if ids[i] != ids[0] {
			t.Errorf("request %d returned adjustment %s, request 0 returned %s", i, ids[i], ids[0])
		}
	}
	if n != 1 {
		t.Errorf("expected exactly 1 creation, got %d", n)
	}
	if got := stockOf(t, pool, p.ID); got != 7 {
		t.Errorf("stock %d, want 7 (applied once)", got)
	}
}

func TestPostgres_StockReservation_ConcurrentRequestsWithOneKeyReserveExactlyOnce(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 10)
	key := newKey()

	const requests = 8
	ids := make([]string, requests)
	created := make([]bool, requests)
	errs := make([]error, requests)
	race(requests, func(i int) {
		var r model.StockReservation
		r, created[i], errs[i] = db.StockReservations().CreateOnce(ctxT(), key, newReservation(t, want(p.ID, 3)))
		ids[i] = r.ID
	})

	n := 0
	for i := range requests {
		if errs[i] != nil {
			t.Fatalf("request %d: %v", i, errs[i])
		}
		if created[i] {
			n++
		}
		if ids[i] != ids[0] {
			t.Errorf("request %d returned reservation %s, request 0 returned %s", i, ids[i], ids[0])
		}
	}
	if n != 1 {
		t.Errorf("expected exactly 1 creation, got %d", n)
	}
	if got := stockOf(t, pool, p.ID); got != 7 {
		t.Errorf("stock %d, want 7 (reserved once)", got)
	}
}

func TestPostgres_StockReservation_ConcurrentReleasesRestoreStockExactlyOnce(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p1 := stockedProduct(t, db, pool, 10)
	p2 := stockedProduct(t, db, pool, 10)
	r := newReservation(t, want(p1.ID, 4), want(p2.ID, 6))
	if _, _, err := db.StockReservations().CreateOnce(ctxT(), newKey(), r); err != nil {
		t.Fatal(err)
	}

	const requests = 8
	errs := make([]error, requests)
	statuses := make([]string, requests)
	race(requests, func(i int) {
		var got model.StockReservation
		got, errs[i] = db.StockReservations().Release(ctxT(), r.ID)
		statuses[i] = got.Status
	})

	for i := range requests {
		if errs[i] != nil || statuses[i] != model.StatusReleased {
			t.Errorf("release %d: status=%q err=%v", i, statuses[i], errs[i])
		}
	}
	if stockOf(t, pool, p1.ID) != 10 || stockOf(t, pool, p2.ID) != 10 {
		t.Errorf("stock p1=%d p2=%d, want 10 and 10 (restored once, not %d times)", stockOf(t, pool, p1.ID), stockOf(t, pool, p2.ID), requests)
	}
}

// A release racing new reservations over the same product must neither
// lose nor invent stock: whatever the interleaving, the books balance.
func TestPostgres_StockReservation_ReleaseRacingNewReservationsKeepsTheBooksBalanced(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 10)
	held := newReservation(t, want(p.ID, 10)) // takes everything
	if _, _, err := db.StockReservations().CreateOnce(ctxT(), newKey(), held); err != nil {
		t.Fatal(err)
	}

	const takers = 12
	results := make([]error, takers)
	var releaseErr error
	race(takers+1, func(i int) {
		if i == takers {
			_, releaseErr = db.StockReservations().Release(ctxT(), held.ID)
			return
		}
		_, _, results[i] = db.StockReservations().CreateOnce(ctxT(), newKey(), newReservation(t, want(p.ID, 1)))
	})

	if releaseErr != nil {
		t.Fatalf("release: %v", releaseErr)
	}
	taken := 0
	for i, err := range results {
		switch {
		case err == nil:
			taken++
		case errors.Is(err, out.ErrInsufficientStock):
		default:
			t.Fatalf("taker %d: unexpected error: %v", i, err)
		}
	}
	// 10 units were restored by the release; every successful taker holds one.
	if got := stockOf(t, pool, p.ID); got != 10-taken {
		t.Errorf("stock %d with %d takers, want %d", got, taken, 10-taken)
	}
	if taken > 10 {
		t.Errorf("%d takers succeeded on 10 units", taken)
	}
}
