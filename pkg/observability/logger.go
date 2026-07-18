package observability

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/trace"
)

// NewLogger returns a structured logger that writes JSON to stdout (for
// `docker logs` / local dev) AND forwards every record to the OTel Logs SDK
// (which exports to SigNoz over OTLP, see Setup in otel.go).
//
// Trace/log correlation: when a handler calls logger.InfoContext(ctx, ...)
// with a context carrying an active span, traceContextHandler below reads
// trace.SpanContextFromContext(ctx) and adds trace_id/span_id attributes to
// the stdout JSON record. The otelslog bridge does the equivalent for the
// OTLP-exported copy automatically, using the same context — that shared
// trace_id is what lets SigNoz jump from a log line to its matching trace
// and back.
func NewLogger(serviceName string) *slog.Logger {
	stdout := &traceContextHandler{next: slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})}
	otlp := otelslog.NewHandler(serviceName)

	return slog.New(&fanoutHandler{handlers: []slog.Handler{stdout, otlp}})
}

// traceContextHandler injects trace_id/span_id into every log record that
// has an active span in its context, before delegating to next.
type traceContextHandler struct {
	next slog.Handler
}

func (h *traceContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *traceContextHandler) Handle(ctx context.Context, record slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		record.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.next.Handle(ctx, record)
}

func (h *traceContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceContextHandler{next: h.next.WithAttrs(attrs)}
}

func (h *traceContextHandler) WithGroup(name string) slog.Handler {
	return &traceContextHandler{next: h.next.WithGroup(name)}
}

// fanoutHandler sends every record to all of its handlers, so logs go to
// both stdout and the OTLP log exporter.
type fanoutHandler struct {
	handlers []slog.Handler
}

func (f *fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range f.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (f *fanoutHandler) Handle(ctx context.Context, record slog.Record) error {
	for _, h := range f.handlers {
		if !h.Enabled(ctx, record.Level) {
			continue
		}
		if err := h.Handle(ctx, record.Clone()); err != nil {
			return err
		}
	}
	return nil
}

func (f *fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		next[i] = h.WithAttrs(attrs)
	}
	return &fanoutHandler{handlers: next}
}

func (f *fanoutHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		next[i] = h.WithGroup(name)
	}
	return &fanoutHandler{handlers: next}
}
