package observability

import (
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ── DB attribute helpers ─────────────────────────────────────────────────────

// WithDBOperation tags a db_* recording with the SQL operation (INSERT, SELECT…).
// Returns metric.MeasurementOption so it works for both Add (counters) and
// Record (histograms).
func WithDBOperation(operation string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("db.operation", operation))
}

// WithDBStatus tags a db_* recording with ok|error.
func WithDBStatus(status string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("db.status", status))
}

// WithDBOperationAndStatus combines both tags in one call.
func WithDBOperationAndStatus(operation, status string) metric.MeasurementOption {
	return metric.WithAttributes(
		attribute.String("db.operation", operation),
		attribute.String("db.status", status),
	)
}

// ── External-call attribute helpers ─────────────────────────────────────────

// WithExternalTarget tags an external_* recording with the logical target name
// (e.g. "pricing-api", "inventory-service").
func WithExternalTarget(target string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("external.target", target))
}

// WithExternalMethod tags an external_* recording with the HTTP method used.
func WithExternalMethod(method string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("http.method", method))
}

// WithExternalAttrs bundles target + method + status_code for external calls.
func WithExternalAttrs(target, method string, statusCode int) metric.MeasurementOption {
	return metric.WithAttributes(
		attribute.String("external.target", target),
		attribute.String("http.method", method),
		attribute.Int("http.status_code", statusCode),
	)
}

// ── Metrics struct ───────────────────────────────────────────────────────────

// Metrics holds every custom application-level instrument this service emits
// on top of what otelhttp already provides automatically.
//
// DB Call Metrics  – granular per-operation observability for SQLite:
//   - db_operation_duration_seconds  histogram  (existing)  latency per op
//   - db_calls_total                 counter    calls by op+status
//   - db_errors_total                counter    DB errors by op
//   - db_rows_affected_total         counter    rows written by op
//   - db_rows_returned_total         counter    rows read on SELECT/LIST
//
// External Call Metrics – stub instruments ready to be wired to real
// outbound HTTP/gRPC calls (pricing API, inventory service, …):
//   - external_call_duration_seconds histogram  latency per target+method
//   - external_calls_total           counter    calls by target+method+status
//   - external_call_errors_total     counter    failures by target+method
type Metrics struct {
	// ── Business / use-case ─────────────────────────────────────────────────
	OrdersCreatedTotal  metric.Int64Counter
	OrderCreateDuration metric.Float64Histogram
	OrderErrorsTotal    metric.Int64Counter

	// ── DB Call Metrics ─────────────────────────────────────────────────────
	DBOperationDuration metric.Float64Histogram // seconds, by db.operation
	DBCallsTotal        metric.Int64Counter     // calls,   by db.operation + db.status
	DBErrorsTotal       metric.Int64Counter     // errors,  by db.operation
	DBRowsAffectedTotal metric.Int64Counter     // rows written (INSERT/UPDATE/DELETE)
	DBRowsReturnedTotal metric.Int64Counter     // rows read    (SELECT)

	// ── External Call Metrics ────────────────────────────────────────────────
	// Wire these in any future HTTP/gRPC client adapter.  They are created at
	// startup so a name typo fails fast rather than silently emitting nothing.
	ExternalCallDuration metric.Float64Histogram // seconds, by external.target + http.method
	ExternalCallsTotal   metric.Int64Counter     // calls,   by target + method + http.status_code
	ExternalCallErrors   metric.Int64Counter     // errors,  by target + method
}

// NewMetrics creates every instrument up front so a typo in a name fails at
// startup rather than mid-request.
func NewMetrics(serviceName string) (*Metrics, error) {
	meter := otel.Meter(serviceName)

	// ── Business / use-case ─────────────────────────────────────────────────

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

	orderErrors, err := meter.Int64Counter(
		"order_errors_total",
		metric.WithDescription("Number of CreateOrder failures, by error type"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create order_errors_total: %w", err)
	}

	// ── DB Call Metrics ─────────────────────────────────────────────────────

	dbDuration, err := meter.Float64Histogram(
		"db_operation_duration_seconds",
		metric.WithDescription("Duration of a single SQLite operation, tagged by db.operation"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create db_operation_duration_seconds: %w", err)
	}

	dbCallsTotal, err := meter.Int64Counter(
		"db_calls_total",
		metric.WithDescription("Total number of SQLite calls, tagged by db.operation and db.status (ok|error)"),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create db_calls_total: %w", err)
	}

	dbErrorsTotal, err := meter.Int64Counter(
		"db_errors_total",
		metric.WithDescription("Total number of SQLite errors, tagged by db.operation"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create db_errors_total: %w", err)
	}

	dbRowsAffected, err := meter.Int64Counter(
		"db_rows_affected_total",
		metric.WithDescription("Cumulative rows written to SQLite (INSERT/UPDATE/DELETE), tagged by db.operation"),
		metric.WithUnit("{row}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create db_rows_affected_total: %w", err)
	}

	dbRowsReturned, err := meter.Int64Counter(
		"db_rows_returned_total",
		metric.WithDescription("Cumulative rows returned by SQLite SELECT operations"),
		metric.WithUnit("{row}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create db_rows_returned_total: %w", err)
	}

	// ── External Call Metrics ────────────────────────────────────────────────

	extDuration, err := meter.Float64Histogram(
		"external_call_duration_seconds",
		metric.WithDescription("Duration of outbound HTTP/gRPC calls to external services, tagged by external.target and http.method"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create external_call_duration_seconds: %w", err)
	}

	extCallsTotal, err := meter.Int64Counter(
		"external_calls_total",
		metric.WithDescription("Total outbound calls to external services, tagged by external.target, http.method, and http.status_code"),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create external_calls_total: %w", err)
	}

	extCallErrors, err := meter.Int64Counter(
		"external_call_errors_total",
		metric.WithDescription("Total failed outbound calls to external services, tagged by external.target and http.method"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create external_call_errors_total: %w", err)
	}

	return &Metrics{
		OrdersCreatedTotal:  ordersCreated,
		OrderCreateDuration: createDuration,
		OrderErrorsTotal:    orderErrors,

		DBOperationDuration: dbDuration,
		DBCallsTotal:        dbCallsTotal,
		DBErrorsTotal:       dbErrorsTotal,
		DBRowsAffectedTotal: dbRowsAffected,
		DBRowsReturnedTotal: dbRowsReturned,

		ExternalCallDuration: extDuration,
		ExternalCallsTotal:   extCallsTotal,
		ExternalCallErrors:   extCallErrors,
	}, nil
}
