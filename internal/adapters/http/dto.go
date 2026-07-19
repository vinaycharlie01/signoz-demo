package http

import (
	"time"

	"github.com/vinaycharlie01/signoz-demo/internal/domain"
)

// createOrderRequest is the wire shape for POST /api/v1/orders. It is kept
// separate from ports.CreateOrderInput on purpose — the HTTP contract and
// the application layer's input type are allowed to diverge without
// touching each other.
type createOrderRequest struct {
	CustomerName string `json:"customer_name"`
	Item         string `json:"item"`
	Quantity     int    `json:"quantity"`
	AmountCents  int64  `json:"amount_cents"`
}

// orderResponse is the wire shape for an Order. No internal IDs or
// high-cardinality fields beyond what a client legitimately needs.
type orderResponse struct {
	ID           string    `json:"id"`
	CustomerName string    `json:"customer_name"`
	Item         string    `json:"item"`
	Quantity     int       `json:"quantity"`
	AmountCents  int64     `json:"amount_cents"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

func toOrderResponse(o *domain.Order) orderResponse {
	return orderResponse{
		ID:           o.ID,
		CustomerName: o.CustomerName,
		Item:         o.Item,
		Quantity:     o.Quantity,
		AmountCents:  o.AmountCents,
		Status:       string(o.Status),
		CreatedAt:    o.CreatedAt,
	}
}

type errorResponse struct {
	Error string `json:"error"`
}
