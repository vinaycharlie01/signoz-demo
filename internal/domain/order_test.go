package domain_test

import (
	"testing"
	"time"

	"github.com/vinaycharlie01/signoz-demo/internal/domain"
)

func TestNewOrder_Valid(t *testing.T) {
	now := time.Now().UTC()
	o, err := domain.NewOrder("id-1", "Alice", "widget", 2, 1999, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.Status != domain.StatusPending {
		t.Errorf("expected new order to start pending, got %s", o.Status)
	}
	if o.CreatedAt != now {
		t.Errorf("expected CreatedAt %v, got %v", now, o.CreatedAt)
	}
}

func TestNewOrder_Invalid(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name        string
		id          string
		customer    string
		item        string
		quantity    int
		amountCents int64
	}{
		{"empty id", "", "Alice", "widget", 1, 100},
		{"empty customer", "id", "", "widget", 1, 100},
		{"empty item", "id", "Alice", "", 1, 100},
		{"zero quantity", "id", "Alice", "widget", 0, 100},
		{"negative quantity", "id", "Alice", "widget", -1, 100},
		{"negative amount", "id", "Alice", "widget", 1, -1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := domain.NewOrder(tc.id, tc.customer, tc.item, tc.quantity, tc.amountCents, now)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			var invalid domain.ErrInvalidOrder
			if _, ok := any(err).(domain.ErrInvalidOrder); !ok {
				t.Errorf("expected ErrInvalidOrder, got %T", err)
			}
			_ = invalid
		})
	}
}

func TestOrder_Confirm(t *testing.T) {
	o, err := domain.NewOrder("id-1", "Alice", "widget", 1, 100, time.Now().UTC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	o.Confirm()
	if o.Status != domain.StatusConfirmed {
		t.Errorf("expected status confirmed, got %s", o.Status)
	}
}
