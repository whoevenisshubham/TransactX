package telemetry

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var defaultDurationBuckets = []float64{0.005, 0.025, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0}

// Histogram tracks bounded latency observation buckets.
type Histogram struct {
	mu      sync.RWMutex
	buckets []float64
	counts  []uint64
	count   uint64
	sum     float64
}

// NewHistogram creates a histogram with the provided upper bounds.
func NewHistogram(buckets []float64) *Histogram {
	if len(buckets) == 0 {
		buckets = defaultDurationBuckets
	}
	sorted := make([]float64, len(buckets))
	copy(sorted, buckets)
	sort.Float64s(sorted)
	return &Histogram{
		buckets: sorted,
		counts:  make([]uint64, len(sorted)),
	}
}

// Observe records a single floating-point duration in seconds.
func (h *Histogram) Observe(v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.count++
	h.sum += v
	for i, bound := range h.buckets {
		if v <= bound {
			h.counts[i]++
		}
	}
}

// Snapshot returns a copy of current histogram stats.
func (h *Histogram) Snapshot() (count uint64, sum float64, buckets map[float64]uint64) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	bMap := make(map[float64]uint64, len(h.buckets))
	for i, b := range h.buckets {
		bMap[b] = h.counts[i]
	}
	return h.count, h.sum, bMap
}

// Metrics tracks operational platform observations in-memory.
type Metrics struct {
	mu sync.RWMutex

	// Route template HTTP observations: key is "METHOD route status"
	httpRequests  map[string]*atomic.Uint64
	httpDurations map[string]*Histogram

	// Exporter telemetry
	ExportedSpans atomic.Uint64
	DroppedSpans  atomic.Uint64

	// Bank calls: key is "bank result"
	bankCalls     map[string]*atomic.Uint64
	bankDurations map[string]*Histogram

	// Recovery observations
	RecoveryAttempts atomic.Uint64
	RecoverySuccess  atomic.Uint64
	RecoveryErrors   atomic.Uint64

	// Start time for uptime reporting
	StartedAt time.Time
}

// NewMetrics initializes an empty metrics registry.
func NewMetrics() *Metrics {
	return &Metrics{
		httpRequests:  make(map[string]*atomic.Uint64),
		httpDurations: make(map[string]*Histogram),
		bankCalls:     make(map[string]*atomic.Uint64),
		bankDurations: make(map[string]*Histogram),
		StartedAt:     time.Now().UTC(),
	}
}

// RecordHTTPRequest records an HTTP execution by method, route template, and status code.
func (m *Metrics) RecordHTTPRequest(method, route string, statusCode int, duration time.Duration) {
	if route == "" {
		route = "unknown"
	}
	key := fmt.Sprintf("%s %s %d", method, route, statusCode)

	m.mu.Lock()
	counter, ok := m.httpRequests[key]
	if !ok {
		counter = &atomic.Uint64{}
		m.httpRequests[key] = counter
	}
	hist, ok := m.httpDurations[key]
	if !ok {
		hist = NewHistogram(defaultDurationBuckets)
		m.httpDurations[key] = hist
	}
	m.mu.Unlock()

	counter.Add(1)
	hist.Observe(duration.Seconds())
}

// RecordBankCall records an external bank request by bank code and outcome result.
func (m *Metrics) RecordBankCall(bankCode, result string, duration time.Duration) {
	key := fmt.Sprintf("%s %s", bankCode, result)
	m.mu.Lock()
	counter, ok := m.bankCalls[key]
	if !ok {
		counter = &atomic.Uint64{}
		m.bankCalls[key] = counter
	}
	hist, ok := m.bankDurations[key]
	if !ok {
		hist = NewHistogram(defaultDurationBuckets)
		m.bankDurations[key] = hist
	}
	m.mu.Unlock()

	counter.Add(1)
	hist.Observe(duration.Seconds())
}

// PrometheusFormat exports metrics formatted in standard Prometheus text exposition format.
func (m *Metrics) PrometheusFormat() string {
	var sb strings.Builder

	sb.WriteString("# HELP transactx_spans_exported_total Total number of spans exported to trace storage\n")
	sb.WriteString("# TYPE transactx_spans_exported_total counter\n")
	sb.WriteString(fmt.Sprintf("transactx_spans_exported_total %d\n", m.ExportedSpans.Load()))

	sb.WriteString("# HELP transactx_spans_dropped_total Total number of spans dropped due to exporter queue overload\n")
	sb.WriteString("# TYPE transactx_spans_dropped_total counter\n")
	sb.WriteString(fmt.Sprintf("transactx_spans_dropped_total %d\n", m.DroppedSpans.Load()))

	sb.WriteString("# HELP transactx_recovery_attempts_total Total background recovery execution attempts\n")
	sb.WriteString("# TYPE transactx_recovery_attempts_total counter\n")
	sb.WriteString(fmt.Sprintf("transactx_recovery_attempts_total %d\n", m.RecoveryAttempts.Load()))

	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.httpRequests) > 0 {
		sb.WriteString("# HELP transactx_http_requests_total Total HTTP requests by method, route template, and status code\n")
		sb.WriteString("# TYPE transactx_http_requests_total counter\n")
		for key, counter := range m.httpRequests {
			parts := strings.SplitN(key, " ", 3)
			if len(parts) == 3 {
				sb.WriteString(fmt.Sprintf("transactx_http_requests_total{method=%q,route=%q,status=%q} %d\n", parts[0], parts[1], parts[2], counter.Load()))
			}
		}
	}

	return sb.String()
}
