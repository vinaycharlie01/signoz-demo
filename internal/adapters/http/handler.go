// Package http is the driving adapter: it translates HTTP requests into
// calls against ports.OrderService and use case results back into HTTP
// responses. It is the only layer allowed to know about net/http.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/vinaycharlie01/signoz-demo/internal/domain"
	"github.com/vinaycharlie01/signoz-demo/internal/ports"
)

// Handler holds only the driving port and a logger — never a concrete
// repository or the OTel SDK directly.
type Handler struct {
	service ports.OrderService
	logger  *slog.Logger
	ready   func(context.Context) error
}

// NewHandler wires the HTTP adapter. ready is called by GET /ready to
// verify the service's dependencies (currently: the SQLite connection).
func NewHandler(service ports.OrderService, logger *slog.Logger, ready func(context.Context) error) *Handler {
	return &Handler{service: service, logger: logger, ready: ready}
}

// CreateOrder handles POST /api/v1/orders.
func (h *Handler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	var req createOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	order, err := h.service.CreateOrder(r.Context(), ports.CreateOrderInput{
		CustomerName: req.CustomerName,
		Item:         req.Item,
		Quantity:     req.Quantity,
		AmountCents:  req.AmountCents,
	})
	if err != nil {
		h.handleServiceError(w, r, err)
		return
	}

	h.writeJSON(w, http.StatusCreated, toOrderResponse(order))
}

// GetOrder handles GET /api/v1/orders/{id}.
func (h *Handler) GetOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	order, err := h.service.GetOrder(r.Context(), id)
	if err != nil {
		h.handleServiceError(w, r, err)
		return
	}
	h.writeJSON(w, http.StatusOK, toOrderResponse(order))
}

// ListOrders handles GET /api/v1/orders.
func (h *Handler) ListOrders(w http.ResponseWriter, r *http.Request) {
	orders, err := h.service.ListOrders(r.Context())
	if err != nil {
		h.handleServiceError(w, r, err)
		return
	}

	out := make([]orderResponse, 0, len(orders))
	for _, o := range orders {
		out = append(out, toOrderResponse(o))
	}
	h.writeJSON(w, http.StatusOK, out)
}

// Health handles GET /health — a pure liveness probe, no dependency checks.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready handles GET /ready — verifies dependencies (the SQLite connection)
// are reachable before reporting ready.
func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	if err := h.ready(r.Context()); err != nil {
		h.logger.ErrorContext(r.Context(), "readiness check failed", slog.String("error", err.Error()))
		h.writeError(w, http.StatusServiceUnavailable, "not ready")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) handleServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var invalid domain.ErrInvalidOrder
	var notFound domain.ErrNotFound

	switch {
	case errors.As(err, &invalid):
		h.writeError(w, http.StatusBadRequest, err.Error())
	case errors.As(err, &notFound):
		h.writeError(w, http.StatusNotFound, err.Error())
	default:
		h.logger.ErrorContext(r.Context(), "unhandled service error", slog.String("error", err.Error()))
		h.writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	h.writeJSON(w, status, errorResponse{Error: message})
}
