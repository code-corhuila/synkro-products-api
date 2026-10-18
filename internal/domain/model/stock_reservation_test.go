package model

import (
	"errors"
	"testing"
)

func TestValidateLines(t *testing.T) {
	tests := []struct {
		name  string
		lines []ReservationLine
		want  error
	}{
		{"one valid line", []ReservationLine{{ProductID: "a", Quantity: 1}}, nil},
		{"several distinct products", []ReservationLine{{ProductID: "a", Quantity: 1}, {ProductID: "b", Quantity: 7}}, nil},
		{"no lines", nil, ErrNoLines},
		{"empty lines", []ReservationLine{}, ErrNoLines},
		{"zero quantity", []ReservationLine{{ProductID: "a", Quantity: 0}}, ErrQuantityNotPositive},
		{"negative quantity", []ReservationLine{{ProductID: "a", Quantity: 1}, {ProductID: "b", Quantity: -2}}, ErrQuantityNotPositive},
		{"same product twice", []ReservationLine{{ProductID: "a", Quantity: 1}, {ProductID: "a", Quantity: 2}}, ErrDuplicateProduct},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateLines(tc.lines); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestNewStockReservation_StartsReservedAndKeepsTheLines(t *testing.T) {
	lines := []ReservationLine{{LineID: "l1", ProductID: "a", Quantity: 2}}
	r, err := NewStockReservation("res-1", lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.ID != "res-1" || r.Status != StatusReserved || len(r.Lines) != 1 || r.Lines[0] != lines[0] {
		t.Errorf("got %+v", r)
	}
	if r.ReleasedAt != nil {
		t.Errorf("a new reservation has no releasedAt, got %v", r.ReleasedAt)
	}
}

func TestNewStockReservation_RunsTheSameValidation(t *testing.T) {
	if _, err := NewStockReservation("res-1", nil); !errors.Is(err, ErrNoLines) {
		t.Fatalf("expected ErrNoLines, got %v", err)
	}
}
