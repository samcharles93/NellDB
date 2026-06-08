// Package server — OpenTelemetry metrics for the nell-engine sync server.
//
// Exposes a /metrics endpoint via the Prometheus exporter.  Counters track
// request volume by endpoint, records accepted/rejected, and HTTP status
// classes.  Histograms track request latency.
//
// Usage:
//
//	m, _ := server.NewMetrics()
//	defer m.Shutdown(context.Background())
//	http.Handle("/metrics", m.Handler())
//	handler := m.Wrap(srv.Handler())
package server

import (
	"context"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// Metrics holds the OTel meter and pre-built instruments.
type Metrics struct {
	meter    metric.Meter
	provider *sdkmetric.MeterProvider

	requestsTotal   metric.Int64Counter
	requests4xx     metric.Int64Counter
	requests5xx     metric.Int64Counter
	recordsAccepted metric.Int64Counter
	recordsRejected metric.Int64Counter
	requestDuration metric.Float64Histogram
}

// NewMetrics initialises the OTel meter with a Prometheus exporter.
// Caller must call Shutdown before exit.
func NewMetrics() (*Metrics, error) {
	exporter, err := prometheus.New()
	if err != nil {
		return nil, err
	}
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	otel.SetMeterProvider(provider)

	meter := provider.Meter("nell-engine")

	m := &Metrics{meter: meter, provider: provider}

	m.requestsTotal, err = meter.Int64Counter(
		"nell.requests.total",
		metric.WithDescription("Total HTTP requests"),
	)
	if err != nil {
		return nil, err
	}
	m.requests4xx, err = meter.Int64Counter(
		"nell.requests.4xx",
		metric.WithDescription("HTTP 4xx responses"),
	)
	if err != nil {
		return nil, err
	}
	m.requests5xx, err = meter.Int64Counter(
		"nell.requests.5xx",
		metric.WithDescription("HTTP 5xx responses"),
	)
	if err != nil {
		return nil, err
	}
	m.recordsAccepted, err = meter.Int64Counter(
		"nell.records.accepted",
		metric.WithDescription("Records accepted (new writes or LWW winners)"),
	)
	if err != nil {
		return nil, err
	}
	m.recordsRejected, err = meter.Int64Counter(
		"nell.records.rejected",
		metric.WithDescription("Records rejected (LWW losers)"),
	)
	if err != nil {
		return nil, err
	}
	m.requestDuration, err = meter.Float64Histogram(
		"nell.request.duration",
		metric.WithDescription("Request latency in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	return m, nil
}

// Shutdown flushes and shuts down the meter provider.
func (m *Metrics) Shutdown(ctx context.Context) error {
	return m.provider.Shutdown(ctx)
}

// Handler returns an http.Handler serving Prometheus text format at /metrics.
func (m *Metrics) Handler() http.Handler {
	return promhttp.Handler()
}

// Wrap returns middleware that records HTTP-level metrics for every request.
func (m *Metrics) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		m.requestsTotal.Add(r.Context(), 1,
			metric.WithAttributes(
				attribute.String("method", r.Method),
				attribute.String("path", r.URL.Path),
			),
		)

		wrapped := &metricsWriter{ResponseWriter: w}
		next.ServeHTTP(wrapped, r)

		elapsed := time.Since(start).Seconds()
		m.requestDuration.Record(r.Context(), elapsed)

		if wrapped.status >= 500 {
			m.requests5xx.Add(r.Context(), 1)
		} else if wrapped.status >= 400 {
			m.requests4xx.Add(r.Context(), 1)
		}
	})
}

// RecordPush records a push operation.  Called from handlePush.
func (m *Metrics) RecordPush(ctx context.Context, accepted, total int) {
	m.recordsAccepted.Add(ctx, int64(accepted))
	m.recordsRejected.Add(ctx, int64(total-accepted))
}

type metricsWriter struct {
	http.ResponseWriter
	status int
}

func (mw *metricsWriter) WriteHeader(status int) {
	mw.status = status
	mw.ResponseWriter.WriteHeader(status)
}

func (mw *metricsWriter) Write(b []byte) (int, error) {
	if mw.status == 0 {
		mw.status = http.StatusOK
	}
	return mw.ResponseWriter.Write(b)
}
