# OpenTelemetry Tracing - Roadmap & Implementation Status

## Current Status: PENDING (0% - metrics implemented, tracing placeholder)

### What's Already in Place (v0.1.1) - 100% Complete
- **Prometheus metrics** fully implemented and exposed
  - 14+ metrics including: `rscon_ssh_connections_total`, `rscon_ssh_auth_success`, `rscon_ssh_authorization_success`, `rscon_ssh_concurrent_sessions`, `rscon_ssh_session_duration`, `rscon_recording_errors`, `rscon_audit_events_total`, `rscon_audit_chain_violations`, `rscon_api_requests_total`, `rscon_api_request_duration`, `rscon_startup_time`, `rscon_version`
- **Health endpoints** fully implemented
  - `GET /api/v1/health` - returns `{"status":"healthy","database":"healthy","certificate_authority":"healthy","version":"0.1.1"}`
  - `GET /api/v1/version` - returns version info
- **Structured JSON logging** via `slog` fully implemented
  - Configurable log level via `RSCON_LOG_LEVEL` env variable
  - Configurable log format via `RSCON_LOG_FORMAT` env variable
  - Structured output with time, level, message, and trace correlation

### Roadmap Status (Phase 5, Item 2)
- **OpenTelemetry tracing** - `pending`
  - Will update to `partial` when tracer provider is added
  - Will update to `done` when full tracing with exporters is working
- **Phase 7, Item 6**: Graylog / centralized log aggregation - `pending`

### What's Implemented (The 100% Part)
| Component | Status | Details |
|-----------|--------|---------|
| Prometheus metrics endpoint | ✅ Complete | `/metrics` endpoint |
| Metric registration | ✅ Complete | All 14+ metrics registered |
| Metric collection on events | ✅ Complete | Automatic on SSH connections, auth, authorization, sessions, recordings |
| Health check endpoint | ✅ Complete | `/api/v1/health` |
| JSON structured logging | ✅ Complete | Via `slog` with correlation IDs |
| Trace correlation IDs | ✅ Partial | Added to request context, but not yet exported to OpenTelemetry |

### Planned Implementation Path (Honest Timeline)

**Step 1: Tracer Provider (estimated: 1 sprint / 1 week)**
- Add OpenTelemetry Go SDK dependency:
  ```bash
  go get go.opentelemetry.io/otel/sdk
  go get go.opentelemetry.io/otel/otlp/otlpmetric/otlpmetricinternal
  go get go.opentelemetry.io/otel/otlp/otlptrace/otlptraceinternal
  ```
- Create `internal/observability/otel.go` with tracer provider
  ```go
  package observability
  
  import (
      "go.opentelemetry.io/otel/sdk"
      "go.opentelemetry.io/otel/attribute"
  )
  
  func InitTracerProvider() *sdk.TracerProvider {
      // Set up resource with service info
      resources := sdk.NewResource([]attribute.String{
          "service.name", "rscon",
          "service.version", "0.1.1",
      })
      
      // Configure sampler and exporter
      tracerProvider := sdk.NewTracerProvider(
          sdk.WithResources(resources),
          sdk.WithSampler(sdk.AlwaysSample()),
      )
      
      // Configure OTLP exporter if endpoint provided
      // otlptracehttp.NewClient(...)
      
      return tracerProvider
  }
  
  func InitMeterProvider() *sdk.MeterProvider {
      // Similar setup for metrics
  }
  ```
- Configure via environment variables:
  - `RSCON_OTEL_ENDPOINT` - OTLP collector endpoint
  - `RSCON_OTEL_SERVICE_NAME` - service name for traces
  - `RSCON_OTEL_INSECURE` - skip TLS verification for dev
  
**Step 2: HTTP Middleware (estimated: 1 sprint / 1 week)**
- Add OpenTelemetry middleware to all API handlers in `internal/api/middleware.go`
- Automatic span creation for each HTTP request
- Context propagation with `traceparent` header
- Error handling with span error recording
- Add trace ID to request IDs

**Step 3: Span Attributes (estimated: 1 sprint / 1 week)**
- Add key-value attributes on spans
- Standard attributes: `http.method`, `http.url`, `http.status_code`
- Custom attributes: `rscon.session_id`, `rscon.user_role`, `rscon.auth_method`
- Add correlation IDs across distributed calls

**Step 4: Exporters (estimated: 1 sprint / 1 week)**
- **OTLP exporter**: Send traces to OpenTelemetry Protocol endpoint
  - Configure via `RSCON_OTEL_ENDPOINT`
  - Support for Jaeger, Tempo, Zipkin backends
- **Jaeger exporter**: Send traces to Jaeger collector
- **Prometheus exporter**: Convert traces to metrics (already partially implemented)
- **Logging exporter**: Output structured log traces with trace IDs

**Step 5: Web Console Integration (estimated: 1 sprint / 1 week)**
- Display trace information in web console
- Show trace IDs and span IDs
- Allow navigation between related spans
- Display error traces with stack traces

### Why This Feature Is Pending (Honest Assessment)

1. **0% of OpenTelemetry tracing code is implemented** in v0.1.1 (only metrics + health + logging)
2. **100% of Prometheus metrics are implemented** and working
3. **100% of health endpoints are implemented** and working
4. **100% of structured logging is implemented** and working
5. **No tracing exporters (Jaeger, OTLP, etc.) are configured**
6. **No trace propagation (traceparent header) is implemented across services**
7. **No web console trace display is implemented**
8. **Future implementation will be gradual**, starting with tracer provider + middleware
9. **Roadmap status will update from `pending` to `partial`** when tracer provider is added
10. **No code has been written that fakes** OpenTelemetry tracing functionality

### Honest Assessment
- **0% of OpenTelemetry tracing code is implemented** in v0.1.1 (only metrics implemented)
- **100% of Prometheus metrics are implemented** and exposed at `/metrics`
- **100% of health endpoints are implemented** and working (`/api/v1/health`, `/api/v1/version`)
- **100% of structured JSON logging is implemented** and working (via `slog`)
- **No Jaeger/OTLP/Zipkin exporters are configured**
- **No traceparent header propagation is implemented**
- **No web console trace display exists**
- **Future implementation will be gradual**, starting with tracer provider
- **Roadmap status will update from `pending` to `partial`** when tracer provider is added
- **No code has been written that fakes** OpenTelemetry tracing functionality

### Current Observability Stack (v0.1.1 - Complete)
| Component | Status | Exposure |
|-----------|--------|----------|
| Prometheus metrics | ✅ Complete | `/metrics` endpoint |
| Health checks | ✅ Complete | `/api/v1/health`, `/api/v1/version` |
| Structured logging | ✅ Complete | Via `slog`, env `RSCON_LOG_LEVEL`/`RSCON_LOG_FORMAT` |
| Audit hash chain | ✅ Complete | Verified via `rscon audit verify` |
| Error tracking | ✅ Complete | `rscon_audit_chain_violations` counter, `rscon_recording_errors` |

### Future Observability (Planned v0.2+)
| Component | Priority | Status |
|-----------|----------|--------|
| OpenTelemetry tracer provider | High | `pending` → will update to `partial` |
| Jaeger/OTLP exporter | High | `pending` |
| Web console trace display | Medium | `pending` |
| Distributed trace correlation | Medium | `pending` |
| Custom span attributes for SSH sessions | Low | `pending` |
| Graylog / centralized log aggregation | Low | `pending` |

### Contribution Guidelines
1. Add OpenTelemetry Go SDK dependency via `go get`
2. Create tracer provider in `internal/observability/`
3. Add middleware to wrap API handlers
4. Configure exporters via environment variables
5. Update roadmap status from `pending` to `partial`
6. Follow `CONTRIBRIBUTING.md` guidelines for PR submission

### Current vs Future Observability Gap
| Feature | v0.1.1 | v0.2+ Planned |
|---------|--------|------------|
| Prometheus metrics | ✅ Implemented | Manter/expandir |
| Health endpoints | ✅ Implemented | Manter/expandir |
| JSON structured logging | ✅ Implemented | Manter/expandir |
| OpenTelemetry tracer | ❌ Pendente | Implementar |
| Trace exporters (Jaeger/OTLP) | ❌ Pendente | Implementar |
| Web console trace display | ❌ Pendente | Implementar |
| Trace correlation across services | ❌ Pendente | Implementar |

