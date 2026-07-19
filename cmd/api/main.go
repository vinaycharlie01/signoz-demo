// Command api is the Order Service's entry point. It is the composition
// root: the only place that imports every layer (domain, application,
// ports, adapters, observability) and wires them together. Nothing below
// this package should ever import cmd/api.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	httpadapter "github.com/vinaycharlie01/signoz-demo/internal/adapters/http"
	"github.com/vinaycharlie01/signoz-demo/internal/adapters/idgen"
	"github.com/vinaycharlie01/signoz-demo/internal/adapters/pricing"
	"github.com/vinaycharlie01/signoz-demo/internal/adapters/sqlite"
	"github.com/vinaycharlie01/signoz-demo/internal/application"
	"github.com/vinaycharlie01/signoz-demo/pkg/config"
	"github.com/vinaycharlie01/signoz-demo/pkg/observability"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	appCfg := config.Load()
	otelCfg := observability.ConfigFromEnv()

	sdk, err := observability.Setup(ctx, otelCfg)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := sdk.Shutdown(shutdownCtx); err != nil {
			slog.Error("otel shutdown error", slog.String("error", err.Error()))
		}
	}()

	logger := observability.NewLogger(otelCfg.ServiceName)
	logger.InfoContext(ctx, "starting signoz-demo order service",
		slog.String("http_addr", appCfg.HTTPAddr),
		slog.String("db_path", appCfg.DBPath),
		slog.String("otlp_endpoint", otelCfg.OTLPEndpoint),
	)

	if err := os.MkdirAll(filepath.Dir(appCfg.DBPath), 0o755); err != nil {
		return err
	}

	metrics, err := observability.NewMetrics(otelCfg.ServiceName)
	if err != nil {
		return err
	}

	repo, err := sqlite.Open(appCfg.DBPath, metrics)
	if err != nil {
		return err
	}
	defer repo.Close()

	pricingClient := pricing.NewClient("", metrics)
	service := application.NewOrderService(repo, idgen.UUID{}, metrics, pricingClient)
	handler := httpadapter.NewHandler(service, logger, repo.Ping)
	router := httpadapter.NewRouter(handler, logger)

	server := &http.Server{
		Addr:              appCfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.InfoContext(ctx, "http server listening", slog.String("addr", appCfg.HTTPAddr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case <-ctx.Done():
		logger.InfoContext(ctx, "shutdown signal received")
	case err := <-serverErr:
		if err != nil {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}
