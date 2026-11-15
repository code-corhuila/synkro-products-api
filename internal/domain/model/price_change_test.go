package model

import (
	"testing"
	"time"
)

func TestPriceChange_RequiresAuthorAndRealPositiveChangeAndUsesUTC(t *testing.T) {
	for _, c := range []struct {
		old, new int64
		by       string
		want     error
	}{
		{100, 200, "", ErrChangedByRequired}, {0, 200, "user", ErrPriceChangeInvalid},
		{100, -1, "user", ErrPriceChangeInvalid}, {100, 100, "user", ErrPriceChangeNoop},
	} {
		if _, err := NewPriceChange("h", "p", c.old, c.new, c.by, time.Now()); err != c.want {
			t.Errorf("got %v, want %v", err, c.want)
		}
	}
	at := time.Date(2026, 1, 1, 10, 0, 0, 0, time.FixedZone("Bogota", -5*3600))
	h, err := NewPriceChange("h", "p", 100, 200, "user", at)
	if err != nil || h.ChangedAt.Location() != time.UTC || !h.ChangedAt.Equal(at) || h.Sequence != 0 {
		t.Fatalf("%+v %v", h, err)
	}
}
