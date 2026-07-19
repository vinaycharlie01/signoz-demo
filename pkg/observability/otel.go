package observability

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"

	otellog "go.opentelemetry.io/otel/log/global"
)

// SDK bundles the three OTel providers this service uses. Call Shutdown
// during graceful shutdown to flush any buffered spans/metrics/logs.
type SDK struct {
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *metric.MeterProvider
	LoggerProvider *log.LoggerProvider

	shutdownFuncs []func(context.Context) error
}

// Shutdown calls every registered shutdown function, collecting all errors
// rather than stopping at the first one, so a slow exporter doesn't prevent
// the others from flushing.
func (s *SDK) Shutdown(ctx context.Context) error {
	var errs []error
	for _, fn := range s.shutdownFuncs {
		if err := fn(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Setup initializes the TracerProvider, MeterProvider, and LoggerProvider
// with an OTLP/gRPC exporter for each signal, registers them as the global
// providers (so any package can call otel.Tracer(...)/otel.Meter(...) or
// use the otelslog bridge), and configures W3C trace-context + baggage
// propagation.
func Setup(ctx context.Context, cfg Config) (*SDK, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			semconv.DeploymentEnvironmentNameKey.String(cfg.Environment),
		),
		resource.WithFromEnv(),
		resource.WithHost(),
		// resource.WithProcess() is intentionally omitted: it calls
		// os/user.Current() which requires cgo (not available in our
		// CGO_DISABLED=1 build) or $USER set in the container environment.
		// Service identity is already fully covered by the attributes above.
	)
	if err != nil {
		return nil, fmt.Errorf("build otel resource: %w", err)
	}

	sdk := &SDK{}

	dialOpts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint)}
	if cfg.Insecure {
		dialOpts = append(dialOpts, otlptracegrpc.WithInsecure())
	}
	traceExporter, err := otlptracegrpc.New(ctx, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("create trace exporter: %w", err)
	}
	sdk.TracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
	)
	sdk.shutdownFuncs = append(sdk.shutdownFuncs, sdk.TracerProvider.Shutdown)
	otel.SetTracerProvider(sdk.TracerProvider)

	metricOpts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint)}
	if cfg.Insecure {
		metricOpts = append(metricOpts, otlpmetricgrpc.WithInsecure())
	}
	metricExporter, err := otlpmetricgrpc.New(ctx, metricOpts...)
	if err != nil {
		return nil, fmt.Errorf("create metric exporter: %w", err)
	}
	sdk.MeterProvider = metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(metricExporter, metric.WithInterval(15*time.Second))),
		metric.WithResource(res),
	)
	sdk.shutdownFuncs = append(sdk.shutdownFuncs, sdk.MeterProvider.Shutdown)
	otel.SetMeterProvider(sdk.MeterProvider)

	logOpts := []otlploggrpc.Option{otlploggrpc.WithEndpoint(cfg.OTLPEndpoint)}
	if cfg.Insecure {
		logOpts = append(logOpts, otlploggrpc.WithInsecure())
	}
	logExporter, err := otlploggrpc.New(ctx, logOpts...)
	if err != nil {
		return nil, fmt.Errorf("create log exporter: %w", err)
	}
	sdk.LoggerProvider = log.NewLoggerProvider(
		log.WithProcessor(log.NewBatchProcessor(logExporter)),
		log.WithResource(res),
	)
	sdk.shutdownFuncs = append(sdk.shutdownFuncs, sdk.LoggerProvider.Shutdown)
	otellog.SetLoggerProvider(sdk.LoggerProvider)

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return sdk, nil
}
