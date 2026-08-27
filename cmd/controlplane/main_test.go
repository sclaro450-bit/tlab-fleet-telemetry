package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadConfigDefaults(t *testing.T) {
	t.Setenv("HOST", "")
	t.Setenv("PORT", "")
	t.Setenv("TELEMETRY_ENDPOINT", "")

	config, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if config.ListenAddress != "0.0.0.0:8080" {
		t.Fatalf("unexpected listen address: %s", config.ListenAddress)
	}
	if config.TelemetryEndpoint != defaultTelemetryEndpoint {
		t.Fatalf("unexpected telemetry endpoint: %s", config.TelemetryEndpoint)
	}
}

func TestLoadConfigRejectsInvalidPort(t *testing.T) {
	t.Setenv("PORT", "invalid")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected invalid PORT error")
	}
}

func TestHealthHandler(t *testing.T) {
	config := applicationConfig{TelemetryEndpoint: defaultTelemetryEndpoint}
	handler := newHandler(config, func(context.Context) error { return nil })
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", response.Code)
	}
}

func TestHealthHandlerReportsUnavailableTelemetry(t *testing.T) {
	config := applicationConfig{TelemetryEndpoint: defaultTelemetryEndpoint}
	handler := newHandler(config, func(context.Context) error { return errors.New("offline") })
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected status: %d", response.Code)
	}
}
