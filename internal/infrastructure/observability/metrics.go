package observability

import "github.com/prometheus/client_golang/prometheus"

// Metrics holds all Prometheus instruments for autodoc.
type Metrics struct {
	RunsTotal               *prometheus.CounterVec
	DocsUpdatedTotal        prometheus.Counter
	MRCreatedTotal          prometheus.Counter
	ACPRequestsTotal        *prometheus.CounterVec
	ACPRequestDuration      prometheus.Histogram
	ValidationFailuresTotal *prometheus.CounterVec
	ChunkedRequestsTotal    prometheus.Counter
	CircuitBreakerState     *prometheus.GaugeVec
}

// NewMetrics creates and registers all metrics with reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		RunsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "autodoc_runs_total",
			Help: "Total number of autodoc runs, partitioned by status.",
		}, []string{"status"}),

		DocsUpdatedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "autodoc_docs_updated_total",
			Help: "Total number of documents updated.",
		}),

		MRCreatedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "autodoc_mr_created_total",
			Help: "Total number of merge requests created.",
		}),

		ACPRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "autodoc_acp_requests_total",
			Help: "Total number of ACP requests, partitioned by status.",
		}, []string{"status"}),

		ACPRequestDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "autodoc_acp_request_duration_seconds",
			Help:    "Duration of ACP requests in seconds.",
			Buckets: prometheus.DefBuckets,
		}),

		ValidationFailuresTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "autodoc_validation_failures_total",
			Help: "Total number of validation failures, partitioned by check type.",
		}, []string{"check"}),

		ChunkedRequestsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "autodoc_chunked_requests_total",
			Help: "Total number of requests that required chunking (diff exceeded context limit).",
		}),

		CircuitBreakerState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "autodoc_circuit_breaker_state",
			Help: "Current state of the circuit breaker (0=closed, 1=half-open, 2=open).",
		}, []string{"component"}),
	}

	reg.MustRegister(
		m.RunsTotal,
		m.DocsUpdatedTotal,
		m.MRCreatedTotal,
		m.ACPRequestsTotal,
		m.ACPRequestDuration,
		m.ValidationFailuresTotal,
		m.ChunkedRequestsTotal,
		m.CircuitBreakerState,
	)

	return m
}

// IncRunsTotal increments the runs counter for the given status.
func (m *Metrics) IncRunsTotal(status string) { m.RunsTotal.WithLabelValues(status).Inc() }

// IncDocsUpdated increments the docs updated counter.
func (m *Metrics) IncDocsUpdated() { m.DocsUpdatedTotal.Inc() }

// IncMRCreated increments the MR created counter.
func (m *Metrics) IncMRCreated() { m.MRCreatedTotal.Inc() }

// IncACPRequests increments the ACP requests counter for the given status.
func (m *Metrics) IncACPRequests(status string) { m.ACPRequestsTotal.WithLabelValues(status).Inc() }

// ObserveACPDuration records an ACP request duration.
func (m *Metrics) ObserveACPDuration(seconds float64) { m.ACPRequestDuration.Observe(seconds) }

// IncValidationFailure increments the validation failure counter for the given check.
func (m *Metrics) IncValidationFailure(check string) {
	m.ValidationFailuresTotal.WithLabelValues(check).Inc()
}

// IncChunkedRequests increments the chunked requests counter.
func (m *Metrics) IncChunkedRequests() { m.ChunkedRequestsTotal.Inc() }

// SetCircuitBreakerState sets the circuit breaker state gauge.
func (m *Metrics) SetCircuitBreakerState(component string, value float64) {
	m.CircuitBreakerState.WithLabelValues(component).Set(value)
}
