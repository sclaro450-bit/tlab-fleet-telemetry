package monitoring

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/teslamotors/fleet-telemetry/config"
	logrus "github.com/teslamotors/fleet-telemetry/logger"
	"github.com/teslamotors/fleet-telemetry/server/airbrake"
)

type statusServer struct {
}

// Status API
func (s *statusServer) Status() func(w http.ResponseWriter, _ *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	}
}

// StartStatusServer initializes the status server on http
func StartStatusServer(config *config.Config, logger *logrus.Logger, airbrakeHandler *airbrake.Handler) error {
	statusServer := &statusServer{}
	mux := http.NewServeMux()
	mux.Handle("/status", airbrakeHandler.WithReporting(http.HandlerFunc(statusServer.Status())))
	mux.Handle("/health", airbrakeHandler.WithReporting(http.HandlerFunc(statusServer.Status())))
	mux.Handle("/healthz", airbrakeHandler.WithReporting(http.HandlerFunc(statusServer.Status())))
	address := fmt.Sprintf("0.0.0.0:%d", config.StatusPort)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("bind health listener on %s: %w", address, err)
	}
	healthServer := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := healthServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			logger.ErrorLog("status", err, nil)
		}
	}()
	logger.ActivityLog("status_server_configured", logrus.LogInfo{"address": address})
	return nil
}
