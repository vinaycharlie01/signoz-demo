package application_test

import (
	"context"
	"sync"
	"testing"

	"github.com/vinaycharlie01/signoz-demo/internal/application"
	"github.com/vinaycharlie01/signoz-demo/internal/demoscenario"
	"github.com/vinaycharlie01/signoz-demo/internal/domain"
	"github.com/vinaycharlie01/signoz-demo/internal/ports"
	"github.com/vinaycharlie01/signoz-demo/pkg/observability"
)

// fakeRepository is an in-memory ports.OrderRepository used only by tests,
// proving the application layer depends on the port, not on SQLite.
type fakeRepository struct {
	mu     sync.Mutex
	orders map[string]*domain.Order
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{orders: make(map[string]*domain.Order)}
}

func (f *fakeRepository) Save(ctx context.Context, order *domain.Order) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.orders[order.ID] = order
	return nil
}

func (f *fakeRepository) FindByID(ctx context.Context, id string) (*domain.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.orders[id]
	if !ok {
		return nil, domain.ErrNotFound{ID: id}
	}
	return o, nil
}

func (f *fakeRepository) List(ctx context.Context) ([]*domain.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*domain.Order, 0, len(f.orders))
	for _, o := range f.orders {
		out = append(out, o)
	}
	return out, nil
}

type fixedID struct{ id string }

func (f fixedID) NewID() string { return f.id }

func newTestService(t *testing.T, repo ports.OrderRepository, id string) *application.OrderService {
	t.Helper()
	metrics, err := observability.NewMetrics("test-service")
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	return application.NewOrderService(repo, fixedID{id: id}, metrics, nil)
}

func TestCreateOrder_Success(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(t, repo, "order-1")

	order, err := svc.CreateOrder(context.Background(), ports.CreateOrderInput{
		CustomerName: "Alice",
		Item:         "widget",
		Quantity:     2,
		AmountCents:  1999,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.Status != domain.StatusConfirmed {
		t.Errorf("expected persisted order to be confirmed, got %s", order.Status)
	}

	stored, err := repo.FindByID(context.Background(), "order-1")
	if err != nil {
		t.Fatalf("expected order to be persisted: %v", err)
	}
	if stored.Status != domain.StatusConfirmed {
		t.Errorf("expected stored order status confirmed, got %s", stored.Status)
	}
}

func TestCreateOrder_InvalidInput(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(t, repo, "order-1")

	_, err := svc.CreateOrder(context.Background(), ports.CreateOrderInput{
		CustomerName: "",
		Item:         "widget",
		Quantity:     1,
		AmountCents:  100,
	})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if _, ok := repo.orders["order-1"]; ok {
		t.Error("expected nothing persisted for invalid input")
	}
}

func TestCreateOrder_ErrorScenario_NeverTouchesRepository(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(t, repo, "order-1")

	ctx := demoscenario.Into(context.Background(), demoscenario.Error)
	_, err := svc.CreateOrder(ctx, ports.CreateOrderInput{
		CustomerName: "Alice",
		Item:         "widget",
		Quantity:     1,
		AmountCents:  100,
	})
	if err == nil {
		t.Fatal("expected simulated error, got nil")
	}
	if len(repo.orders) != 0 {
		t.Errorf("expected repository untouched, got %d orders", len(repo.orders))
	}
}

func TestGetOrder_NotFound(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(t, repo, "unused")

	_, err := svc.GetOrder(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected not found error, got nil")
	}
	if _, ok := err.(domain.ErrNotFound); !ok {
		t.Errorf("expected domain.ErrNotFound, got %T", err)
	}
}

func TestListOrders_Empty(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(t, repo, "unused")

	orders, err := svc.ListOrders(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(orders) != 0 {
		t.Errorf("expected no orders, got %d", len(orders))
	}
}
