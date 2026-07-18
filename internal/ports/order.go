// Package ports defines the interfaces at the boundary of the hexagon.
// Driving ports (OrderService) are implemented by internal/application and
// called by driving adapters (internal/adapters/http). Driven ports
// (OrderRepository) are called by internal/application and implemented by
// driven adapters (internal/adapters/sqlite).
package ports

import (
	"context"

	"github.com/vinaycharlie01/signoz-demo/internal/domain"
)

// CreateOrderInput is the driving-port request shape. It intentionally
// mirrors the HTTP request body but lives here so the application layer
// never depends on the HTTP adapter's types.
type CreateOrderInput struct {
	CustomerName string
	Item         string
	Quantity     int
	AmountCents  int64
}

// OrderService is the driving port: what the outside world (HTTP today,
// anything else tomorrow) can ask the application to do.
type OrderService interface {
	CreateOrder(ctx context.Context, in CreateOrderInput) (*domain.Order, error)
	GetOrder(ctx context.Context, id string) (*domain.Order, error)
	ListOrders(ctx context.Context) ([]*domain.Order, error)
}

// OrderRepository is the driven port: what the application needs from
// persistence, independent of which database implements it.
type OrderRepository interface {
	Save(ctx context.Context, order *domain.Order) error
	FindByID(ctx context.Context, id string) (*domain.Order, error)
	List(ctx context.Context) ([]*domain.Order, error)
}

// IDGenerator is a driven port so the application layer stays deterministic
// and testable instead of calling a global UUID function directly.
type IDGenerator interface {
	NewID() string
}
