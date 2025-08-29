package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/grafana/plexporter/pkg/log"
	"github.com/grafana/plexporter/pkg/metrics"
	"github.com/grafana/plexporter/pkg/plex"
)

const (
	MetricsServerAddr = ":9000"
)

var (
	logger = createLogger()
)

// createLogger creates the appropriate logger based on environment
func createLogger() log.Logger {
	// Use development logging when explicitly requested (better for local dev)
	if os.Getenv("LOG_FORMAT") == "console" || os.Getenv("ENVIRONMENT") == "development" {
		return log.NewDevelopmentLogger()
	}
	// Default to production JSON logger (better for containers and log aggregation)
	return log.NewProductionLogger()
}

// maskToken returns a masked representation of the token suitable for logs.
// It preserves the last 4 characters when possible and replaces the rest with '*'.
func maskToken(t string) string {
	if t == "" {
		return "(unset)"
	}
	if len(t) <= 4 {
		return "****"
	}
	// keep last 4 characters visible
	return strings.Repeat("*", len(t)-4) + t[len(t)-4:]
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	serverAddress := os.Getenv("PLEX_SERVER")
	if serverAddress == "" {
		logger.Error("PLEX_SERVER environment variable must be specified")
		cancel()
		os.Exit(1)
	}

	plexToken := os.Getenv("PLEX_TOKEN")
	if plexToken == "" {
		logger.Error("PLEX_TOKEN environment variable must be specified")
		cancel()
		os.Exit(1)
	}

	// Support TZ environment variable: if set, attempt to load and apply it
	tzEnv := os.Getenv("TZ")
	tzResolved := "(system)"
	if tzEnv != "" {
		if loc, err := time.LoadLocation(tzEnv); err != nil {
			logger.Warn("invalid TZ, using system default", zap.String("TZ", tzEnv), zap.Error(err))
			tzResolved = "invalid: " + tzEnv
		} else {
			time.Local = loc
			tzResolved = loc.String()
		}
	}

	// Log key environment variables at startup (mask sensitive values)
	libRefresh := os.Getenv("LIBRARY_REFRESH_INTERVAL")
	if libRefresh == "" {
		libRefresh = "15 (minutes, default; 0 = disable caching)"
	} else {
		libRefresh += " (minutes; 0 = disable caching)"
	}
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}
	skipTLS := os.Getenv("SKIP_TLS_VERIFICATION")
	if skipTLS == "" {
		skipTLS = "false"
	}

	logger.Info("starting exporter",
		zap.String("PLEX_SERVER", serverAddress),
		zap.String("PLEX_TOKEN", maskToken(plexToken)),
		zap.String("LIBRARY_REFRESH_INTERVAL", libRefresh),
		zap.String("LOG_LEVEL", logLevel),
		zap.String("SKIP_TLS_VERIFICATION", skipTLS),
		zap.String("TZ", tzResolved),
	)

	server, err := plex.NewServer(serverAddress, plexToken)
	if err != nil {
		logger.Error("cannot initialize connection to plex server", zap.Error(err))
		os.Exit(1)
	}

	metrics.Register(server)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	metricsServer := http.Server{
		Addr:         MetricsServerAddr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		logger.Info("starting metrics server on " + MetricsServerAddr)
		err = metricsServer.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("cannot start metrics server", zap.Error(err))
		}
	}()

	exitCode := 0
	err = server.Listen(ctx, logger)
	if err != nil {
		logger.Error("cannot listen to plex server events", zap.Error(err))
		exitCode = 1
	}

	logger.Debug("shutting down metrics server")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("cannot gracefully shutdown metrics server", zap.Error(err))
	}

	// ensure shutdown cancel is called before exiting so any resources are
	// released; avoid relying on deferred calls that won't run after os.Exit
	shutdownCancel()
	os.Exit(exitCode)
}
