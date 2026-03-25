package observability

import "github.com/prometheus/client_golang/prometheus"

// Metrics holds all Prometheus instruments for autodoc.
type Metrics struct {
	RunsTotal              *prometheus.CounterVec
	DocsUpdatedTotal       prometheus.Counter
	MRCreatedTotal         prometheus.Counter
	ACPRequestsTotal       *prometheus.CounterVec
	ACPRequestDuration     prometheus.Histogram
	ValidationFailuresTotal *prometheus.CounterVec
	ChunkedRequestsTotal   prometheus.Counter
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
	}

	reg.MustRegister(
		m.RunsTotal,
		m.DocsUpdatedTotal,
		m.MRCreatedTotal,
		m.ACPRequestsTotal,
		m.ACPRequestDuration,
		m.ValidationFailuresTotal,
		m.ChunkedRequestsTotal,
	)

	return m
}
