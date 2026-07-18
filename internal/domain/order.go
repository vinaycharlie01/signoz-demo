// Package domain holds the Order Service's business model. It has zero
// dependencies on HTTP frameworks, database drivers, or OpenTelemetry —
// those all belong to adapters and ports, never here.
package domain

import (
	"strings"
	"time"
)

// Status is the lifecycle state of an Order.
type Status string

const (
	StatusPending   Status = "pending"
	StatusConfirmed Status = "confirmed"
	StatusFailed    Status = "failed"
)

// Order is the aggregate root for this domain.
type Order struct {
	ID           string
	CustomerName string
	Item         string
	Quantity     int
	AmountCents  int64
	Status       Status
	CreatedAt    time.Time
}

// NewOrder validates input and constructs a pending Order. This is the only
// place an Order is allowed to come into existence, so invariants live here.
func NewOrder(id, customerName, item string, quantity int, amountCents int64, now time.Time) (*Order, error) {
	customerName = strings.TrimSpace(customerName)
	item = strings.TrimSpace(item)

	if id == "" {
		return nil, ErrInvalidOrder("id must not be empty")
	}
	if customerName == "" {
		return nil, ErrInvalidOrder("customer_name must not be empty")
	}
	if item == "" {
		return nil, ErrInvalidOrder("item must not be empty")
	}
	if quantity <= 0 {
		return nil, ErrInvalidOrder("quantity must be greater than zero")
	}
	if amountCents < 0 {
		return nil, ErrInvalidOrder("amount_cents must not be negative")
	}

	return &Order{
		ID:           id,
		CustomerName: customerName,
		Item:         item,
		Quantity:     quantity,
		AmountCents:  amountCents,
		Status:       StatusPending,
		CreatedAt:    now,
	}, nil
}

// Confirm transitions an order to confirmed. Kept as a method (rather than
// a free-standing setter) so future rules about valid transitions have one
// home.
func (o *Order) Confirm() {
	o.Status = StatusConfirmed
}
