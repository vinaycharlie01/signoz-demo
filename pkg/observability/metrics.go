package observability

import (
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// WithDBOperation tags a db_operation_duration_seconds recording with which
// SQL operation (INSERT, SELECT, ...) it measured.
func WithDBOperation(operation string) metric.RecordOption {
	return metric.WithAttributes(attribute.String("db.operation", operation))
}

// Metrics holds the custom application-level instruments this service emits
// on top of what otelhttp already provides automatically (http.server.*
// request count/duration, recorded by the HTTP adapter's middleware).
type Metrics struct {
	OrdersCreatedTotal  metric.Int64Counter
	OrderCreateDuration metric.Float64Histogram
	DBOperationDuration metric.Float64Histogram
	OrderErrorsTotal    metric.Int64Counter
}

// NewMetrics creates every instrument up front (rather than lazily) so a
// typo in an instrument name fails at startup, not mid-request.
func NewMetrics(serviceName string) (*Metrics, error) {
	meter := otel.Meter(serviceName)

	ordersCreated, err := meter.Int64Counter(
		"orders_created_total",
		metric.WithDescription("Number of orders successfully created"),
		metric.WithUnit("{order}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create orders_created_total: %w", err)
	}

	createDuration, err := meter.Float64Histogram(
		"order_create_duration_seconds",
		metric.WithDescription("End-to-end duration of the CreateOrder use case"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create order_create_duration_seconds: %w", err)
	}

	dbDuration, err := meter.Float64Histogram(
		"db_operation_duration_seconds",
		metric.WithDescription("Duration of a single SQLite operation, by db.operation"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create db_operation_duration_seconds: %w", err)
	}

	orderErrors, err := meter.Int64Counter(
		"order_errors_total",
		metric.WithDescription("Number of CreateOrder failures, by error type"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create order_errors_total: %w", err)
	}

	return &Metrics{
		OrdersCreatedTotal:  ordersCreated,
		OrderCreateDuration: createDuration,
		DBOperationDuration: dbDuration,
		OrderErrorsTotal:    orderErrors,
	}, nil
}
