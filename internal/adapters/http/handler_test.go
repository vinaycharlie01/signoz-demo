package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	httpadapter "github.com/vinaycharlie01/signoz-demo/internal/adapters/http"
	"github.com/vinaycharlie01/signoz-demo/internal/domain"
	"github.com/vinaycharlie01/signoz-demo/internal/ports"
)

// fakeService is an in-memory ports.OrderService double, proving the HTTP
// adapter depends only on the port, never on the concrete application type.
type fakeService struct {
	createFn func(ctx context.Context, in ports.CreateOrderInput) (*domain.Order, error)
	getFn    func(ctx context.Context, id string) (*domain.Order, error)
	listFn   func(ctx context.Context) ([]*domain.Order, error)
}

func (f *fakeService) CreateOrder(ctx context.Context, in ports.CreateOrderInput) (*domain.Order, error) {
	return f.createFn(ctx, in)
}
func (f *fakeService) GetOrder(ctx context.Context, id string) (*domain.Order, error) {
	return f.getFn(ctx, id)
}
func (f *fakeService) ListOrders(ctx context.Context) ([]*domain.Order, error) {
	return f.listFn(ctx)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nil_Writer{}, nil))
}

type nil_Writer struct{}

func (nil_Writer) Write(p []byte) (int, error) { return len(p), nil }

func TestCreateOrder_Success(t *testing.T) {
	svc := &fakeService{
		createFn: func(ctx context.Context, in ports.CreateOrderInput) (*domain.Order, error) {
			return &domain.Order{ID: "id-1", CustomerName: in.CustomerName, Item: in.Item, Quantity: in.Quantity, AmountCents: in.AmountCents, Status: domain.StatusConfirmed, CreatedAt: time.Now()}, nil
		},
	}
	router := httpadapter.NewRouter(httpadapter.NewHandler(svc, testLogger(), func(context.Context) error { return nil }), testLogger())

	body, _ := json.Marshal(map[string]any{"customer_name": "Alice", "item": "widget", "quantity": 1, "amount_cents": 100})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateOrder_InvalidJSON(t *testing.T) {
	svc := &fakeService{createFn: func(ctx context.Context, in ports.CreateOrderInput) (*domain.Order, error) { return nil, nil }}
	router := httpadapter.NewRouter(httpadapter.NewHandler(svc, testLogger(), func(context.Context) error { return nil }), testLogger())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader([]byte("{not json")))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateOrder_DomainValidationError(t *testing.T) {
	svc := &fakeService{
		createFn: func(ctx context.Context, in ports.CreateOrderInput) (*domain.Order, error) {
			return nil, domain.ErrInvalidOrder("quantity must be greater than zero")
		},
	}
	router := httpadapter.NewRouter(httpadapter.NewHandler(svc, testLogger(), func(context.Context) error { return nil }), testLogger())

	body, _ := json.Marshal(map[string]any{"customer_name": "Alice", "item": "widget", "quantity": 0, "amount_cents": 100})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetOrder_NotFound(t *testing.T) {
	svc := &fakeService{
		getFn: func(ctx context.Context, id string) (*domain.Order, error) { return nil, domain.ErrNotFound{ID: id} },
	}
	router := httpadapter.NewRouter(httpadapter.NewHandler(svc, testLogger(), func(context.Context) error { return nil }), testLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/missing", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestListOrders_Empty(t *testing.T) {
	svc := &fakeService{listFn: func(ctx context.Context) ([]*domain.Order, error) { return nil, nil }}
	router := httpadapter.NewRouter(httpadapter.NewHandler(svc, testLogger(), func(context.Context) error { return nil }), testLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "[]\n" {
		t.Errorf("expected empty JSON array, got %q", rec.Body.String())
	}
}

func TestHealthAndReady(t *testing.T) {
	svc := &fakeService{}
	router := httpadapter.NewRouter(httpadapter.NewHandler(svc, testLogger(), func(context.Context) error { return nil }), testLogger())

	for _, path := range []string{"/health", "/ready"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", path, rec.Code)
		}
	}
}

func TestReady_Failure(t *testing.T) {
	svc := &fakeService{}
	router := httpadapter.NewRouter(httpadapter.NewHandler(svc, testLogger(), func(context.Context) error { return context.DeadlineExceeded }), testLogger())

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}
