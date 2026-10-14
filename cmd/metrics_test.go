package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Fankhauserli/voabkr-backend/handlers"
	"github.com/gin-gonic/gin"
)

func TestMetricsEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := handlers.NewHandler(nil, nil)
	router := setupRouter(h, nil)

	// Make a healthz request to populate some metrics
	wHealth := httptest.NewRecorder()
	reqHealth, _ := http.NewRequest(http.MethodGet, "/healthz", nil)
	router.ServeHTTP(wHealth, reqHealth)
	if wHealth.Code != http.StatusOK {
		t.Fatalf("expected /healthz 200, got %d", wHealth.Code)
	}

	// Request /metrics
	wMetrics := httptest.NewRecorder()
	reqMetrics, _ := http.NewRequest(http.MethodGet, "/metrics", nil)
	router.ServeHTTP(wMetrics, reqMetrics)

	if wMetrics.Code != http.StatusOK {
		t.Fatalf("expected /metrics 200, got %d: %s", wMetrics.Code, wMetrics.Body.String())
	}

	body := wMetrics.Body.String()
	if !strings.Contains(body, "http_requests_total") {
		t.Errorf("expected /metrics to contain 'http_requests_total', got:\n%s", body)
	}
	if !strings.Contains(body, "http_request_duration_seconds") {
		t.Errorf("expected /metrics to contain 'http_request_duration_seconds', got:\n%s", body)
	}
}
