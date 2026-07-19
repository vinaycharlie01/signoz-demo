// Package pricing is a simulated driven adapter for an external pricing service.
// In this demo there is no real pricing service, so the client calls httpbin.org
// purely to generate realistic external_call_* OTel metric data that can be
// visualised in SigNoz.  A real service would replace this with its actual
// HTTP/gRPC client.
package pricing

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/vinaycharlie01/signoz-demo/pkg/observability"
)

const tracerName = "signoz-demo/internal/adapters/pricing"

// Client is the driven adapter for the external pricing service.
// It satisfies application.Pricer and records external_call_* metrics.
type Client struct {
	httpClient *http.Client
	baseURL    string
	metrics    *observability.Metrics
}

// NewClient creates a pricing client.
// Pass baseURL="" to use the default demo endpoint (httpbin.org).
func NewClient(baseURL string, metrics *observability.Metrics) *Client {
	if baseURL == "" {
		baseURL = "https://httpbin.org"
	}
	return &Client{
		httpClient: &http.Client{Timeout: 3 * time.Second},
		baseURL:    baseURL,
		metrics:    metrics,
	}
}

// GetPrice calls the external pricing service and records latency/call/error metrics.
// Signature matches application.Pricer (returns error only).
// ~20% of calls hit /status/500 to generate realistic error data for the dashboard.
func (c *Client) GetPrice(ctx context.Context, item string) error {
	start := time.Now()

	// Rotate between healthy and error endpoints.
	endpoint := "/status/200"
	if time.Now().UnixNano()%5 == 0 {
		endpoint = "/status/500"
	}

	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "pricing.GetPrice",
		trace.WithAttributes(
			attribute.String("pricing.item", item),
			attribute.String("pricing.endpoint", endpoint),
		),
	)
	defer span.End()

	url := c.baseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		dur := time.Since(start).Seconds()
		c.recordCall(ctx, 0, dur, err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("build pricing request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	dur := time.Since(start).Seconds()
	if err != nil {
		c.recordCall(ctx, 0, dur, err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("call pricing service: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	var callErr error
	if resp.StatusCode >= 500 {
		callErr = fmt.Errorf("pricing service returned %d", resp.StatusCode)
		span.RecordError(callErr)
		span.SetStatus(codes.Error, callErr.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}

	c.recordCall(ctx, resp.StatusCode, dur, callErr)
	return callErr
}

// recordCall records all three external_call_* instruments for pricing-service.
func (c *Client) recordCall(ctx context.Context, statusCode int, duration float64, callErr error) {
	withAll := metric.WithAttributes(
		attribute.String("external.target", "pricing-service"),
		attribute.String("http.method", http.MethodGet),
		attribute.Int("http.status_code", statusCode),
	)
	withoutStatus := metric.WithAttributes(
		attribute.String("external.target", "pricing-service"),
		attribute.String("http.method", http.MethodGet),
	)

	c.metrics.ExternalCallDuration.Record(ctx, duration, withAll)
	c.metrics.ExternalCallsTotal.Add(ctx, 1, withAll)
	if callErr != nil {
		c.metrics.ExternalCallErrors.Add(ctx, 1, withoutStatus)
	}
}
