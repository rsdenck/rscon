// Package observability exposes Prometheus metrics for RSCON.
package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds the RSCON metric collectors.
type Metrics struct {
	Registry *prometheus.Registry

	SSHConnections  *prometheus.CounterVec
	SSHAuthFailures *prometheus.CounterVec
	SSHSessions     *prometheus.CounterVec
	SSHActive       prometheus.Gauge
	AuthzDenied     *prometheus.CounterVec
	CertIssued      prometheus.Counter
	CertRevoked     prometheus.Counter
	VaultFailures   prometheus.Counter
	RecordingErrors prometheus.Counter
	AuditErrors     prometheus.Counter
	APIRequests     *prometheus.CounterVec
	APILatency      *prometheus.HistogramVec
}

// New registers and returns metrics.
func New() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		Registry: reg,
		SSHConnections: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rscon_ssh_connections_total", Help: "SSH connections handled, by result.",
		}, []string{"result"}),
		SSHAuthFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rscon_ssh_auth_failures_total", Help: "Failed SSH authentications, by reason.",
		}, []string{"reason"}),
		SSHSessions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rscon_ssh_sessions_total", Help: "SSH sessions completed, by result.",
		}, []string{"result"}),
		SSHActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "rscon_ssh_active_sessions", Help: "Interactive SSH sessions currently proxied.",
		}),
		AuthzDenied: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rscon_authorization_denied_total", Help: "Authorization denials, by action.",
		}, []string{"action"}),
		CertIssued: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "rscon_certificates_issued_total", Help: "SSH certificates issued.",
		}),
		CertRevoked: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "rscon_certificates_revoked_total", Help: "SSH certificates revoked.",
		}),
		VaultFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "rscon_vault_failures_total", Help: "Vault request failures.",
		}),
		RecordingErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "rscon_recording_failures_total", Help: "Session recording failures.",
		}),
		AuditErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "rscon_audit_write_failures_total", Help: "Audit persistence failures.",
		}),
		APIRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rscon_api_requests_total", Help: "HTTP API requests, by route and status.",
		}, []string{"method", "route", "code"}),
		APILatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "rscon_api_request_duration_seconds",
			Help:    "HTTP API request latency.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
	}
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.SSHConnections, m.SSHAuthFailures, m.SSHSessions, m.SSHActive,
		m.AuthzDenied, m.CertIssued, m.CertRevoked, m.VaultFailures,
		m.RecordingErrors, m.AuditErrors, m.APIRequests, m.APILatency,
	)
	return m
}

// Handler serves the metrics endpoint.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}

// ObserveAPI records a request. Route must be a bounded template, not a raw
// path containing identifiers.
func (m *Metrics) ObserveAPI(method, route string, status int, started time.Time) {
	m.APIRequests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
	m.APILatency.WithLabelValues(method, route).Observe(time.Since(started).Seconds())
}
