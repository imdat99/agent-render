package http

import (
	"strconv"
	"time"

	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// HTTP metrics
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	// Job metrics
	jobsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "jobs_total",
			Help: "Total number of jobs by status",
		},
		[]string{"status"},
	)

	jobsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "jobs_active",
			Help: "Number of currently active jobs",
		},
	)

	jobDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "job_duration_seconds",
			Help:    "Job execution duration in seconds",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300, 600, 1800, 3600},
		},
	)

	// Agent metrics
	agentsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "agents_active",
			Help: "Number of currently active agents",
		},
	)

	agentCPUUsage = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "agent_cpu_usage_percent",
			Help: "Agent CPU usage percentage",
		},
		[]string{"agent_id"},
	)

	agentRAMUsage = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "agent_ram_usage_mb",
			Help: "Agent RAM usage in MB",
		},
		[]string{"agent_id"},
	)

	// Queue metrics
	queueDepth = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "queue_depth",
			Help: "Number of jobs waiting in queue",
		},
	)
)

// MetricsMiddleware records HTTP request metrics
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip metrics for WebSocket upgrade requests
		if r.Header.Get("Upgrade") == "websocket" {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()

		// Wrap ResponseWriter to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start).Seconds()
		path := chi.RouteContext(r.Context()).RoutePattern()
		if path == "" {
			path = r.URL.Path
		}

		httpRequestsTotal.WithLabelValues(r.Method, path, strconv.Itoa(wrapped.statusCode)).Inc()
		httpRequestDuration.WithLabelValues(r.Method, path).Observe(duration)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// MetricsHandler returns the Prometheus metrics handler
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}

// RecordJobCreated increments job creation counter
func RecordJobCreated(status string) {
	jobsTotal.WithLabelValues(status).Inc()
}

// RecordJobCompleted records job completion
func RecordJobCompleted(status string, duration time.Duration) {
	jobsTotal.WithLabelValues(status).Inc()
	jobDuration.Observe(duration.Seconds())
}

// SetActiveJobs sets the current number of active jobs
func SetActiveJobs(count int) {
	jobsActive.Set(float64(count))
}

// SetActiveAgents sets the current number of active agents
func SetActiveAgents(count int) {
	agentsActive.Set(float64(count))
}

// SetQueueDepth sets the current queue depth
func SetQueueDepth(depth int) {
	queueDepth.Set(float64(depth))
}

// RecordAgentResources records agent CPU and RAM usage
func RecordAgentResources(agentID string, cpu, ram float64) {
	agentCPUUsage.WithLabelValues(agentID).Set(cpu)
	agentRAMUsage.WithLabelValues(agentID).Set(ram)
}
