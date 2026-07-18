// Package application holds the use cases (driving-port implementations).
// It depends on internal/domain and internal/ports only, plus the OpenTelemetry
// API (not domain) to create the "use case" span/metrics layer the blog's
// debugging story walks through.
package application

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/vinaycharlie01/signoz-demo/internal/demoscenario"
	"github.com/vinaycharlie01/signoz-demo/internal/domain"
	"github.com/vinaycharlie01/signoz-demo/internal/ports"
	"github.com/vinaycharlie01/signoz-demo/pkg/observability"
)

const tracerName = "signoz-demo/internal/application"

// OrderService implements ports.OrderService. It is the only layer allowed
// to know both the domain model and the repository port at once.
type OrderService struct {
	repo    ports.OrderRepository
	ids     ports.IDGenerator
	metrics *observability.Metrics
}

// NewOrderService wires the use case with its driven-port dependencies.
func NewOrderService(repo ports.OrderRepository, ids ports.IDGenerator, metrics *observability.Metrics) *OrderService {
	return &OrderService{repo: repo, ids: ids, metrics: metrics}
}

// CreateOrder validates and persists a new order. Scenario "error" (see
// internal/demoscenario) fails here, before the repository is ever called —
// deliberately, so the resulting trace shows an error span at the use-case
// layer with no child DB span, distinct from a "db-fail" scenario.
func (s *OrderService) CreateOrder(ctx context.Context, in ports.CreateOrderInput) (order *domain.Order, err error) {
	start := time.Now()
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "OrderService.CreateOrder", trace.WithAttributes(
		attribute.String("order.customer_name", in.CustomerName),
		attribute.String("order.item", in.Item),
		attribute.Int("order.quantity", in.Quantity),
	))
	defer func() {
		duration := time.Since(start).Seconds()
		s.metrics.OrderCreateDuration.Record(ctx, duration)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			s.metrics.OrderErrorsTotal.Add(ctx, 1)
		} else {
			span.SetStatus(codes.Ok, "")
			span.SetAttributes(attribute.String("order.id", order.ID))
			s.metrics.OrdersCreatedTotal.Add(ctx, 1)
		}
		span.End()
	}()

	if demoscenario.From(ctx) == demoscenario.Error {
		return nil, domain.ErrInvalidOrder("simulated application error (X-Demo-Scenario: error)")
	}

	newOrder, err := domain.NewOrder(s.ids.NewID(), in.CustomerName, in.Item, in.Quantity, in.AmountCents, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	newOrder.Confirm()

	if err := s.repo.Save(ctx, newOrder); err != nil {
		return nil, err
	}
	return newOrder, nil
}

// GetOrder fetches a single order by ID.
func (s *OrderService) GetOrder(ctx context.Context, id string) (*domain.Order, error) {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "OrderService.GetOrder", trace.WithAttributes(attribute.String("order.id", id)))
	defer span.End()

	order, err := s.repo.FindByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	return order, nil
}

// ListOrders returns every order. Fine for a demo; a real service would
// paginate.
func (s *OrderService) ListOrders(ctx context.Context) ([]*domain.Order, error) {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "OrderService.ListOrders")
	defer span.End()

	orders, err := s.repo.List(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(attribute.Int("order.count", len(orders)))
	return orders, nil
}
