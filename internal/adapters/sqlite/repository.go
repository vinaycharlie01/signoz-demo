// Package sqlite is a driven adapter: it implements ports.OrderRepository
// against a local SQLite database. Nothing outside this package (and
// cmd/api, which wires it) knows or cares that SQLite is the storage engine.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver registered as "sqlite"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/vinaycharlie01/signoz-demo/internal/demoscenario"
	"github.com/vinaycharlie01/signoz-demo/internal/domain"
	"github.com/vinaycharlie01/signoz-demo/pkg/observability"
)

const tracerName = "signoz-demo/internal/adapters/sqlite"

const schema = `
CREATE TABLE IF NOT EXISTS orders (
	id            TEXT PRIMARY KEY,
	customer_name TEXT NOT NULL,
	item          TEXT NOT NULL,
	quantity      INTEGER NOT NULL,
	amount_cents  INTEGER NOT NULL,
	status        TEXT NOT NULL,
	created_at    TEXT NOT NULL
);`

// injectedSlowness is how long the "slow" demo scenario sleeps for inside
// the DB span, simulating a slow query/lock/contended connection pool.
const injectedSlowness = 1800 * time.Millisecond

// Repository implements ports.OrderRepository backed by database/sql + the
// modernc.org/sqlite driver (no cgo, so it cross-compiles cleanly in the
// Dockerfile's multi-stage build).
type Repository struct {
	db      *sql.DB
	metrics *observability.Metrics
}

// Open opens (creating if necessary) the SQLite file at path and applies
// the schema.
func Open(path string, metrics *observability.Metrics) (*Repository, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database %q: %w", path, err)
	}
	db.SetMaxOpenConns(1) // modernc.org/sqlite: keep writes serialized

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	return &Repository{db: db, metrics: metrics}, nil
}

// Close closes the underlying database handle.
func (r *Repository) Close() error { return r.db.Close() }

// Ping verifies the database connection is alive, used by GET /ready.
func (r *Repository) Ping(ctx context.Context) error { return r.db.PingContext(ctx) }

func (r *Repository) startSpan(ctx context.Context, operation, statement string) (context.Context, trace.Span) {
	tracer := otel.Tracer(tracerName)
	return tracer.Start(ctx, "sqlite."+operation+" orders", trace.WithAttributes(
		attribute.String("db.system", "sqlite"),
		attribute.String("db.operation", operation),
		attribute.String("db.sql.table", "orders"),
		attribute.String("db.statement", statement),
	))
}

func (r *Repository) recordResult(ctx context.Context, span trace.Span, operation string, start time.Time, err error) {
	duration := time.Since(start).Seconds()
	r.metrics.DBOperationDuration.Record(ctx, duration, observability.WithDBOperation(operation))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End()
}

// Save inserts a new order. The "slow" scenario sleeps before the INSERT so
// the extra latency lands squarely on this span, not the HTTP or use-case
// span. The "db-fail" scenario returns an error without touching the
// database at all, simulating a downed/unreachable DB.
func (r *Repository) Save(ctx context.Context, order *domain.Order) error {
	start := time.Now()
	ctx, span := r.startSpan(ctx, "INSERT", "INSERT INTO orders (...) VALUES (...)")

	var err error
	defer func() { r.recordResult(ctx, span, "INSERT", start, err) }()

	switch demoscenario.From(ctx) {
	case demoscenario.Slow:
		span.SetAttributes(attribute.Bool("demo.scenario.slow", true))
		time.Sleep(injectedSlowness)
	case demoscenario.DBFail:
		span.SetAttributes(attribute.Bool("demo.scenario.db_fail", true))
		err = fmt.Errorf("simulated database outage (X-Demo-Scenario: db-fail)")
		return err
	}

	_, err = r.db.ExecContext(ctx,
		`INSERT INTO orders (id, customer_name, item, quantity, amount_cents, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		order.ID, order.CustomerName, order.Item, order.Quantity, order.AmountCents, string(order.Status), order.CreatedAt.Format(time.RFC3339Nano),
	)
	return err
}

// FindByID reads a single order back out.
func (r *Repository) FindByID(ctx context.Context, id string) (*domain.Order, error) {
	start := time.Now()
	ctx, span := r.startSpan(ctx, "SELECT", "SELECT ... FROM orders WHERE id = ?")

	var err error
	defer func() { r.recordResult(ctx, span, "SELECT", start, err) }()

	row := r.db.QueryRowContext(ctx,
		`SELECT id, customer_name, item, quantity, amount_cents, status, created_at FROM orders WHERE id = ?`, id)

	order, scanErr := scanOrder(row.Scan)
	if scanErr == sql.ErrNoRows {
		err = domain.ErrNotFound{ID: id}
		return nil, err
	}
	if scanErr != nil {
		err = scanErr
		return nil, err
	}
	return order, nil
}

// List returns every order, most recent first.
func (r *Repository) List(ctx context.Context) ([]*domain.Order, error) {
	start := time.Now()
	ctx, span := r.startSpan(ctx, "SELECT", "SELECT ... FROM orders ORDER BY created_at DESC")

	var err error
	defer func() { r.recordResult(ctx, span, "SELECT", start, err) }()

	rows, queryErr := r.db.QueryContext(ctx,
		`SELECT id, customer_name, item, quantity, amount_cents, status, created_at FROM orders ORDER BY created_at DESC`)
	if queryErr != nil {
		err = queryErr
		return nil, err
	}
	defer rows.Close()

	var orders []*domain.Order
	for rows.Next() {
		order, scanErr := scanOrder(rows.Scan)
		if scanErr != nil {
			err = scanErr
			return nil, err
		}
		orders = append(orders, order)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		err = rowsErr
		return nil, err
	}
	span.SetAttributes(attribute.Int("db.rows_returned", len(orders)))
	return orders, nil
}

func scanOrder(scan func(dest ...any) error) (*domain.Order, error) {
	var (
		o         domain.Order
		status    string
		createdAt string
	)
	if err := scan(&o.ID, &o.CustomerName, &o.Item, &o.Quantity, &o.AmountCents, &status, &createdAt); err != nil {
		return nil, err
	}
	o.Status = domain.Status(status)
	parsed, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	o.CreatedAt = parsed
	return &o, nil
}
