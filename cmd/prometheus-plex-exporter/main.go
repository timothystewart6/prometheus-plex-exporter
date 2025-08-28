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

	kitlog "github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/grafana/plexporter/pkg/metrics"
	"github.com/grafana/plexporter/pkg/plex"
)

const (
	MetricsServerAddr = ":9000"
)

var (
	log = kitlog.NewLogfmtLogger(kitlog.NewSyncWriter(os.Stderr))
)

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
	defer cancel()

	serverAddress := os.Getenv("PLEX_SERVER")
	if serverAddress == "" {
		_ = level.Error(log).Log("msg", "PLEX_SERVER environment variable must be specified")
		os.Exit(1)
	}

	plexToken := os.Getenv("PLEX_TOKEN")
	if plexToken == "" {
		_ = level.Error(log).Log("msg", "PLEX_TOKEN environment variable must be specified")
		os.Exit(1)
	}

	// Support TZ environment variable: if set, attempt to load and apply it
	tzEnv := os.Getenv("TZ")
	tzResolved := "(system)"
	if tzEnv != "" {
		if loc, err := time.LoadLocation(tzEnv); err != nil {
			_ = level.Warn(log).Log("msg", "invalid TZ, using system default", "TZ", tzEnv, "error", err)
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
		libRefresh = libRefresh + " (minutes; 0 = disable caching)"
	}
	debugFlag := os.Getenv("DEBUG")
	if debugFlag == "" {
		debugFlag = "false"
	}
	skipTLS := os.Getenv("SKIP_TLS_VERIFICATION")
	if skipTLS == "" {
		skipTLS = "false"
	}

	_ = level.Info(log).Log(
		"msg", "starting exporter",
		"PLEX_SERVER", serverAddress,
		"PLEX_TOKEN", maskToken(plexToken),
		"LIBRARY_REFRESH_INTERVAL", libRefresh,
		"DEBUG", debugFlag,
		"SKIP_TLS_VERIFICATION", skipTLS,
		"TZ", tzResolved,
	)

	server, err := plex.NewServer(serverAddress, plexToken)
	if err != nil {
		_ = level.Error(log).Log("msg", "cannot initialize connection to plex server", "error", err)
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
		_ = level.Info(log).Log("msg", "starting metrics server on "+MetricsServerAddr)
		err = metricsServer.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			_ = level.Error(log).Log("msg", "cannot start metrics server", "error", err)
		}
	}()

	exitCode := 0
	err = server.Listen(ctx, log)
	if err != nil {
		_ = level.Error(log).Log("msg", "cannot listen to plex server events", "error", err)
		exitCode = 1
	}

	_ = level.Debug(log).Log("msg", "shutting down metrics server")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		_ = level.Error(log).Log("msg", "cannot gracefully shutdown metrics server", "error", err)
	}

	os.Exit(exitCode)
}
