package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/vinaycharlie01/signoz-demo/internal/adapters/sqlite"
	"github.com/vinaycharlie01/signoz-demo/internal/demoscenario"
	"github.com/vinaycharlie01/signoz-demo/internal/domain"
	"github.com/vinaycharlie01/signoz-demo/pkg/observability"
)

func newTestRepo(t *testing.T) *sqlite.Repository {
	t.Helper()
	metrics, err := observability.NewMetrics("test-service")
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	path := filepath.Join(t.TempDir(), "orders.db")
	repo, err := sqlite.Open(path, metrics)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	return repo
}

func TestRepository_SaveAndFindByID(t *testing.T) {
	repo := newTestRepo(t)
	order, err := domain.NewOrder("id-1", "Alice", "widget", 2, 1999, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	order.Confirm()

	if err := repo.Save(context.Background(), order); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByID(context.Background(), "id-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.CustomerName != "Alice" || got.Status != domain.StatusConfirmed {
		t.Errorf("unexpected order: %+v", got)
	}
}

func TestRepository_FindByID_NotFound(t *testing.T) {
	repo := newTestRepo(t)
	_, err := repo.FindByID(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, ok := err.(domain.ErrNotFound); !ok {
		t.Errorf("expected domain.ErrNotFound, got %T", err)
	}
}

func TestRepository_DBFailScenario(t *testing.T) {
	repo := newTestRepo(t)
	order, err := domain.NewOrder("id-1", "Alice", "widget", 1, 100, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}

	ctx := demoscenario.Into(context.Background(), demoscenario.DBFail)
	if err := repo.Save(ctx, order); err == nil {
		t.Fatal("expected simulated db-fail error, got nil")
	}

	if _, err := repo.FindByID(context.Background(), "id-1"); err == nil {
		t.Fatal("expected order to not have been persisted")
	}
}

func TestRepository_SlowScenario_AddsLatency(t *testing.T) {
	repo := newTestRepo(t)
	order, err := domain.NewOrder("id-1", "Alice", "widget", 1, 100, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}

	ctx := demoscenario.Into(context.Background(), demoscenario.Slow)
	start := time.Now()
	if err := repo.Save(ctx, order); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Errorf("expected slow scenario to add >=1s latency, took %s", elapsed)
	}
}

func TestRepository_List(t *testing.T) {
	repo := newTestRepo(t)
	for i, id := range []string{"id-1", "id-2"} {
		order, err := domain.NewOrder(id, "Alice", "widget", i+1, 100, time.Now().UTC())
		if err != nil {
			t.Fatalf("NewOrder: %v", err)
		}
		if err := repo.Save(context.Background(), order); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	orders, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(orders) != 2 {
		t.Fatalf("expected 2 orders, got %d", len(orders))
	}
}
