package http

import (
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// NewRouter builds the full HTTP handler tree: routing, then per-route
// otelhttp instrumentation (HTTP server spans + otelhttp's built-in
// http.server.request.duration/count metrics), then the demo-scenario and
// access-log middleware around everything.
func NewRouter(h *Handler, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	mux.Handle("POST /api/v1/orders", otelhttp.NewHandler(http.HandlerFunc(h.CreateOrder), "POST /api/v1/orders"))
	mux.Handle("GET /api/v1/orders/{id}", otelhttp.NewHandler(http.HandlerFunc(h.GetOrder), "GET /api/v1/orders/{id}"))
	mux.Handle("GET /api/v1/orders", otelhttp.NewHandler(http.HandlerFunc(h.ListOrders), "GET /api/v1/orders"))
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("GET /ready", h.Ready)

	return withScenario(withAccessLog(logger, mux))
}

// withAccessLog logs one structured line per request, correlated to the
// request's trace via the active span in its context (see
// pkg/observability.NewLogger for how trace_id/span_id get attached).
func withAccessLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		logger.InfoContext(r.Context(), "http request",
			slog.String("http.method", r.Method),
			slog.String("http.path", r.URL.Path),
			slog.Int("http.status_code", rec.status),
			slog.Duration("duration", time.Since(start)),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
