// Package observability wires the OpenTelemetry SDK (traces, metrics, logs)
// for this service. It is imported only by cmd/api and the adapters layer —
// never by internal/domain, and only by internal/application where a span
// or metric needs to be recorded around a use case.
package observability

import "os"

// Config controls how the OTel SDK exports telemetry. All fields have safe
// defaults for local development against the docker-compose stack in this
// repo.
type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	OTLPEndpoint   string // host:port, gRPC, no scheme — e.g. "localhost:4317"
	Insecure       bool
}

// ConfigFromEnv reads OTEL_* / SERVICE_* environment variables, falling back
// to defaults that match docker-compose.yml.
func ConfigFromEnv() Config {
	return Config{
		ServiceName:    getEnv("OTEL_SERVICE_NAME", "signoz-demo-order-service"),
		ServiceVersion: getEnv("SERVICE_VERSION", "0.1.0"),
		Environment:    getEnv("DEPLOYMENT_ENVIRONMENT", "local"),
		OTLPEndpoint:   getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
		Insecure:       getEnv("OTEL_EXPORTER_OTLP_INSECURE", "true") == "true",
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
