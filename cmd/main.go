package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "go.uber.org/automaxprocs"

	"github.com/airbrake/gobrake/v5"
	"github.com/teslamotors/fleet-telemetry/config"
	logrus "github.com/teslamotors/fleet-telemetry/logger"
	"github.com/teslamotors/fleet-telemetry/server/airbrake"
	"github.com/teslamotors/fleet-telemetry/server/monitoring"
	"github.com/teslamotors/fleet-telemetry/server/streaming"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "fleet-telemetry startup failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	serviceConfig, logger, err := config.LoadApplicationConfiguration()
	if err != nil {
		return fmt.Errorf("CONFIGURATION ERROR: %w", err)
	}
	if err := serviceConfig.ValidateRuntime(); err != nil {
		return fmt.Errorf("CONFIGURATION ERROR: %w", err)
	}
	defer serviceConfig.MetricCollector.Shutdown()

	if serviceConfig.Monitoring != nil && serviceConfig.Monitoring.ProfilingPath != "" {
		if serviceConfig.Monitoring.ProfilerFile, err = os.Create(serviceConfig.Monitoring.ProfilingPath); err != nil {
			logger.ErrorLog("profiling_file_error", err, nil)
			serviceConfig.Monitoring.ProfilingPath = ""
		} else {
			defer serviceConfig.Monitoring.ProfilerFile.Close()
		}
	}

	airbrakeNotifier, _, err := serviceConfig.CreateAirbrakeNotifier(logger)
	if err != nil {
		return fmt.Errorf("configure Airbrake: %w", err)
	}
	if airbrakeNotifier != nil {
		defer airbrakeNotifier.NotifyOnPanic()
		defer func() {
			if err := airbrakeNotifier.Close(); err != nil {
				logger.ErrorLog("airbrake_close_error", err, nil)
			}
		}()
	}
	return startServer(serviceConfig, airbrakeNotifier, logger)
}

func startServer(config *config.Config, airbrakeNotifier *gobrake.Notifier, logger *logrus.Logger) (err error) {
	logger.ActivityLog("starting_server", nil)
	registry := streaming.NewSocketRegistry()

	airbrakeHandler := airbrake.NewAirbrakeHandler(airbrakeNotifier)

	if config.Monitoring != nil {
		monitoring.StartServerMetrics(config, logger, registry)
	}

	dispatchers, producerRules, err := config.ConfigureProducers(airbrakeHandler, logger, false)
	if err != nil {
		return err
	}
	server, _, err := streaming.InitServer(config, airbrakeHandler, producerRules, logger, registry)
	if err != nil {
		return err
	}

	if server.TLSConfig, err = config.ExtractServiceTLSConfig(logger); err != nil {
		return fmt.Errorf("CONFIGURATION ERROR: initialize Tesla mTLS client verification: %w", err)
	}
	serverCertificate, err := tls.LoadX509KeyPair(config.TLS.ServerCert, config.TLS.ServerKey)
	if err != nil {
		return fmt.Errorf("CONFIGURATION ERROR: load TLS certificate and private key: %w", err)
	}
	server.TLSConfig.Certificates = []tls.Certificate{serverCertificate}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("telemetry listener bind failed on %s: %w", server.Addr, err)
	}
	tlsListener := tls.NewListener(listener, server.TLSConfig)
	if config.StatusPort > 0 {
		if err := monitoring.StartStatusServer(config, logger, airbrakeHandler); err != nil {
			_ = listener.Close()
			return err
		}
	}
	logger.ActivityLog("telemetry_listener_configured", logrus.LogInfo{
		"address":     server.Addr,
		"tls_enabled": true,
	})

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.Serve(tlsListener)
	}()

	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(shutdownSignals)

	select {
	case err = <-serverErrors:
	case receivedSignal := <-shutdownSignals:
		logger.ActivityLog("shutdown_requested", logrus.LogInfo{"signal": receivedSignal.String()})
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err = server.Shutdown(shutdownContext)
	}
	for dispatcher, producer := range dispatchers {
		logger.ActivityLog("attempting_to_close", logrus.LogInfo{"dispatcher": dispatcher})
		// We don't care if this fails. If it does, we'll just continue on.
		if dispatcherCloseErr := producer.Close(); dispatcherCloseErr != nil {
			logger.ErrorLog("producer_close_error", dispatcherCloseErr, logrus.LogInfo{"dispatcher": dispatcher})
		}
	}
	logger.ActivityLog("stopped_server", nil)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("telemetry listener failed on %s: %w", server.Addr, err)
	}
	return err
}
