// Package config loads this service's own runtime configuration (port,
// database path). OpenTelemetry configuration lives separately in
// pkg/observability, since it is conceptually a different concern.
package config

import "os"

// App holds cmd/api's configuration.
type App struct {
	HTTPAddr string
	DBPath   string
}

// Load reads configuration from the environment, with defaults matching
// docker-compose.yml / local `go run` usage.
func Load() App {
	return App{
		HTTPAddr: getEnv("HTTP_ADDR", ":8090"),
		DBPath:   getEnv("DB_PATH", "./data/orders.db"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
