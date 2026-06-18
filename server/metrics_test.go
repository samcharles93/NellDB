package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewMetrics(t *testing.T) {
	m, err := NewMetrics()
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	if m.meter == nil {
		t.Error("meter is nil")
	}
	if m.provider == nil {
		t.Error("provider is nil")
	}
	if m.requestsTotal == nil {
		t.Error("requestsTotal counter is nil")
	}
	if m.requests4xx == nil {
		t.Error("requests4xx counter is nil")
	}
	if m.requests5xx == nil {
		t.Error("requests5xx counter is nil")
	}
	if m.recordsAccepted == nil {
		t.Error("recordsAccepted counter is nil")
	}
	if m.recordsRejected == nil {
		t.Error("recordsRejected counter is nil")
	}
	if m.requestDuration == nil {
		t.Error("requestDuration histogram is nil")
	}

	// Shutdown should succeed.
	if err := m.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

func TestMetricsHandler(t *testing.T) {
	m, err := NewMetrics()
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	defer m.Shutdown(context.Background())

	h := m.Handler()
	if h == nil {
		t.Error("Handler returned nil")
	}

	// Serve a request to /metrics — should return Prometheus text.
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("metrics handler: status = %d, want 200", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Error("metrics handler returned empty body")
	}
}

func TestMetricsWrap(t *testing.T) {
	m, err := NewMetrics()
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	defer m.Shutdown(context.Background())

	called := false
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/sync/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("wrapped handler was not called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestMetricsWrap4xx(t *testing.T) {
	m, err := NewMetrics()
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	defer m.Shutdown(context.Background())

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestMetricsWrap5xx(t *testing.T) {
	m, err := NewMetrics()
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	defer m.Shutdown(context.Background())

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	req := httptest.NewRequest(http.MethodGet, "/sync/broken", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestMetricsRecordPush(t *testing.T) {
	m, err := NewMetrics()
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	defer m.Shutdown(context.Background())

	// RecordPush should not panic.
	m.RecordPush(context.Background(), 5, 7)
	m.RecordPush(context.Background(), 0, 0)
	m.RecordPush(context.Background(), 10, 10)
}

func TestMetricsWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	mw := &metricsWriter{ResponseWriter: rec}

	// Write before WriteHeader should set status to 200.
	mw.Write([]byte("hello"))
	if mw.status != http.StatusOK {
		t.Errorf("implicit status = %d, want 200", mw.status)
	}
}

func TestMetricsWriterExplicitStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	mw := &metricsWriter{ResponseWriter: rec}

	// WriteHeader before Write.
	mw.WriteHeader(http.StatusCreated)
	if mw.status != http.StatusCreated {
		t.Errorf("status = %d, want 201", mw.status)
	}
	mw.Write([]byte("created"))
	if mw.status != http.StatusCreated {
		t.Errorf("status after Write = %d, want 201", mw.status)
	}
}

func TestMetricsWriterWriteHeaderOnly(t *testing.T) {
	rec := httptest.NewRecorder()
	mw := &metricsWriter{ResponseWriter: rec}

	// WriteHeader without Write.
	mw.WriteHeader(http.StatusNoContent)
	if mw.status != http.StatusNoContent {
		t.Errorf("status = %d, want 204", mw.status)
	}
	// Write after WriteHeader should not change status.
	mw.Write([]byte("ignored"))
	if mw.status != http.StatusNoContent {
		t.Errorf("status after Write = %d, want 204", mw.status)
	}
}
