// Package audit persists security events to a hash-chained table and fans them
// out to structured logs and optional syslog/GELF collectors.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/rsdenck/rscon/internal/identity"
	"github.com/rsdenck/rscon/internal/model"
	"github.com/rsdenck/rscon/internal/observability"
	"github.com/rsdenck/rscon/internal/store"
)

// Results.
const (
	ResultSuccess = "success"
	ResultDenied  = "denied"
	ResultFailure = "failure"
)

// Categories.
const (
	CategoryAuth        = "authentication"
	CategoryAuthz       = "authorization"
	CategorySSH         = "ssh"
	CategorySession     = "session"
	CategoryCertificate = "certificate"
	CategoryAdmin       = "admin"
	CategoryApproval    = "approval"
	CategoryVault       = "vault"
	CategoryAudit       = "audit"
	CategorySystem      = "system"
	CategorySecurity    = "security"
)

// Event is an audit event before persistence.
type Event struct {
	Category      string
	Action        string
	ActorID       string
	ActorName     string
	Result        string
	ResourceType  string
	ResourceID    string
	SourceIP      string
	CorrelationID string
	Message       string
	Details       map[string]any
}

// SyslogConfig configures external fan-out.
type SyslogConfig struct {
	Enabled bool
	Network string
	Address string
	Format  string
}

// Config configures the audit service.
type Config struct {
	Store          *store.Store
	Logger         *slog.Logger
	Metrics        *observability.Metrics
	RequirePersist bool
	Host           string
	Syslog         SyslogConfig
}

// Service writes audit events.
type Service struct {
	store   *store.Store
	log     *slog.Logger
	metrics *observability.Metrics
	require bool
	host    string
	sink    *syslogSink
}

// New creates an audit service.
func New(cfg Config) (*Service, error) {
	s := &Service{
		store:   cfg.Store,
		log:     cfg.Logger,
		metrics: cfg.Metrics,
		require: cfg.RequirePersist,
		host:    cfg.Host,
	}
	if cfg.Syslog.Enabled {
		sink, err := newSyslogSink(cfg.Syslog, cfg.Host)
		if err != nil {
			return nil, err
		}
		s.sink = sink
	}
	return s, nil
}

// Write persists and fans out an event. When RequirePersist is true, a
// database failure is returned so the caller can fail the privileged
// operation. External sink failures are logged but never block the operation.
func (s *Service) Write(ctx context.Context, e Event) error {
	if e.Result == "" {
		e.Result = ResultSuccess
	}
	event := &model.AuditEvent{
		EventID:       identity.NewID(),
		Category:      e.Category,
		Action:        e.Action,
		ActorID:       e.ActorID,
		ActorName:     e.ActorName,
		Result:        e.Result,
		ResourceType:  e.ResourceType,
		ResourceID:    e.ResourceID,
		SourceIP:      e.SourceIP,
		CorrelationID: e.CorrelationID,
		Message:       e.Message,
		Details:       e.Details,
	}
	if s.store != nil {
		if err := s.store.AppendAudit(ctx, event); err != nil {
			if s.metrics != nil {
				s.metrics.AuditErrors.Inc()
			}
			if s.log != nil {
				s.log.Error("audit persistence failed", "action", e.Action, "category", e.Category, "error", err)
			}
			if s.require {
				return fmt.Errorf("audit persistence is mandatory: %w", err)
			}
		}
	}
	if s.log != nil {
		attrs := []any{
			"event_id", event.EventID, "category", e.Category, "action", e.Action,
			"actor_id", e.ActorID, "actor", e.ActorName, "result", e.Result,
			"resource_type", e.ResourceType, "resource_id", e.ResourceID,
			"source_ip", e.SourceIP, "correlation_id", e.CorrelationID,
		}
		if e.Message != "" {
			attrs = append(attrs, "message", e.Message)
		}
		if e.Result == ResultFailure || e.Result == ResultDenied {
			s.log.Warn("audit event", attrs...)
		} else {
			s.log.Info("audit event", attrs...)
		}
	}
	if s.sink != nil {
		if err := s.sink.Send(ctx, event); err != nil && s.log != nil {
			s.log.Warn("audit sink delivery failed", "error", err, "sink", s.sink.kind)
		}
	}
	return nil
}

// Close releases external sink resources.
func (s *Service) Close() error {
	if s.sink != nil {
		return s.sink.Close()
	}
	return nil
}

type syslogSink struct {
	kind    string
	network string
	address string
	host    string
}

func newSyslogSink(cfg SyslogConfig, host string) (*syslogSink, error) {
	if cfg.Network != "udp" && cfg.Network != "tcp" {
		return nil, errors.New("audit: syslog network must be udp or tcp")
	}
	if cfg.Format != "json" && cfg.Format != "gelf" {
		return nil, errors.New("audit: syslog format must be json or gelf")
	}
	if cfg.Address == "" {
		return nil, errors.New("audit: syslog address is required")
	}
	return &syslogSink{kind: cfg.Format, network: cfg.Network, address: cfg.Address, host: host}, nil
}

func (s *syslogSink) Send(ctx context.Context, e *model.AuditEvent) error {
	var payload []byte
	var err error
	if s.kind == "gelf" {
		level := 6
		if e.Result != ResultSuccess {
			level = 4
		}
		msg := map[string]any{
			"version":         "1.1",
			"host":            s.host,
			"short_message":   e.Action,
			"timestamp":       float64(e.CreatedAt.UnixNano()) / 1e9,
			"level":           level,
			"_event_id":       e.EventID,
			"_category":       e.Category,
			"_actor_id":       e.ActorID,
			"_actor":          e.ActorName,
			"_result":         e.Result,
			"_resource_type":  e.ResourceType,
			"_resource_id":    e.ResourceID,
			"_source_ip":      e.SourceIP,
			"_correlation_id": e.CorrelationID,
		}
		if e.Message != "" {
			msg["_message"] = e.Message
		}
		if len(e.Details) > 0 {
			if b, err := json.Marshal(e.Details); err == nil {
				msg["_details"] = string(b)
			}
		}
		payload, err = json.Marshal(msg)
	} else {
		payload, err = json.Marshal(map[string]any{
			"host": s.host, "event": e,
		})
	}
	if err != nil {
		return err
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, s.network, s.address)
	if err != nil {
		return fmt.Errorf("audit: connect %s: %w", s.address, err)
	}
	defer conn.Close()
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("audit: send %s: %w", s.address, err)
	}
	return nil
}

func (s *syslogSink) Close() error { return nil }
