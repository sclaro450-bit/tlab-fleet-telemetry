package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultHost              = "0.0.0.0"
	defaultPort              = 8080
	defaultTelemetryEndpoint = "telemetry.tlabcontrol.com:37532"
)

var errCertificateVerified = errors.New("certificate verified")

type applicationConfig struct {
	ListenAddress     string
	TelemetryEndpoint string
	TelemetryHost     string
}

type healthResponse struct {
	Status            string `json:"status"`
	Component         string `json:"component"`
	TelemetryEndpoint string `json:"telemetry_endpoint"`
	Error             string `json:"error,omitempty"`
}

func main() {
	if err := run(); err != nil {
		slog.Error("control_plane_stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	config, err := loadConfig()
	if err != nil {
		return fmt.Errorf("CONFIGURATION ERROR: %w", err)
	}

	handler := newHandler(config, func(ctx context.Context) error {
		return verifyTelemetryTLS(ctx, config.TelemetryEndpoint, config.TelemetryHost)
	})
	server := &http.Server{
		Addr:              config.ListenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("control_plane_started", "address", config.ListenAddress, "telemetry_endpoint", config.TelemetryEndpoint)
		serverErrors <- server.ListenAndServe()
	}()

	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(shutdownSignals)

	select {
	case err = <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case receivedSignal := <-shutdownSignals:
		slog.Info("control_plane_shutdown_requested", "signal", receivedSignal.String())
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownContext)
	}
}

func loadConfig() (applicationConfig, error) {
	host := strings.TrimSpace(os.Getenv("HOST"))
	if host == "" {
		host = defaultHost
	}
	port := defaultPort
	if rawPort := strings.TrimSpace(os.Getenv("PORT")); rawPort != "" {
		parsedPort, err := strconv.Atoi(rawPort)
		if err != nil || parsedPort < 1 || parsedPort > 65535 {
			return applicationConfig{}, fmt.Errorf("invalid PORT %q", rawPort)
		}
		port = parsedPort
	}

	endpoint := strings.TrimSpace(os.Getenv("TELEMETRY_ENDPOINT"))
	if endpoint == "" {
		endpoint = defaultTelemetryEndpoint
	}
	telemetryHost, rawPort, err := net.SplitHostPort(endpoint)
	if err != nil || strings.TrimSpace(telemetryHost) == "" {
		return applicationConfig{}, fmt.Errorf("invalid TELEMETRY_ENDPOINT %q: expected host:port", endpoint)
	}
	telemetryPort, err := strconv.Atoi(rawPort)
	if err != nil || telemetryPort < 1 || telemetryPort > 65535 {
		return applicationConfig{}, fmt.Errorf("invalid TELEMETRY_ENDPOINT port %q", rawPort)
	}

	return applicationConfig{
		ListenAddress:     net.JoinHostPort(host, strconv.Itoa(port)),
		TelemetryEndpoint: endpoint,
		TelemetryHost:     telemetryHost,
	}, nil
}

func newHandler(config applicationConfig, probe func(context.Context) error) http.Handler {
	mux := http.NewServeMux()
	healthHandler := func(writer http.ResponseWriter, request *http.Request) {
		probeContext, cancel := context.WithTimeout(request.Context(), 7*time.Second)
		defer cancel()
		response := healthResponse{
			Status:            "healthy",
			Component:         "vercel-control-plane",
			TelemetryEndpoint: config.TelemetryEndpoint,
		}
		statusCode := http.StatusOK
		if err := probe(probeContext); err != nil {
			statusCode = http.StatusServiceUnavailable
			response.Status = "unhealthy"
			response.Error = "persistent telemetry TLS endpoint is unavailable"
			slog.Warn("telemetry_health_check_failed", "telemetry_endpoint", config.TelemetryEndpoint, "error", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(statusCode)
		_ = json.NewEncoder(writer).Encode(response)
	}
	mux.HandleFunc("GET /", healthHandler)
	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("GET /healthz", healthHandler)
	return mux
}

func verifyTelemetryTLS(ctx context.Context, endpoint, serverName string) error {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	rawConnection, err := dialer.DialContext(ctx, "tcp", endpoint)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", endpoint, err)
	}
	connection := tls.Client(rawConnection, &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: serverName,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.VerifiedChains) == 0 {
				return errors.New("server certificate chain was not verified")
			}
			return errCertificateVerified
		},
	})
	defer connection.Close()
	err = connection.HandshakeContext(ctx)
	if errors.Is(err, errCertificateVerified) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("verify %s: %w", endpoint, err)
	}
	return nil
}
