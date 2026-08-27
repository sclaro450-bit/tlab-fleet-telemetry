package monitoring

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	(&statusServer{}).Status()(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("expected application/json, got %q", contentType)
	}
	if body := recorder.Body.String(); body != "{\"status\":\"healthy\"}\n" {
		t.Fatalf("unexpected health response %q", body)
	}
}
