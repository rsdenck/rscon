# OpenTelemetry Tracing - Pending Implementation

## Status: PENDING (not yet implemented)

This feature is documented as pending in the roadmap and will be implemented in a future release.

### Current State
- **No OpenTelemetry tracing** is implemented in RSCON v0.1
- **Metrics** are exposed via Prometheus (see `internal/observability/`), but they are **not traced**
- **Tracing** (distributed tracing with trace IDs, span IDs, etc.) is **not yet implemented**
- All OpenTelemetry support is listed as `pending` in `docs/development/roadmap.md`

### Current Observability Features (implemented)
- **Prometheus metrics** (`internal/observability/`):
  - `rscon_ssh_connections_total` - total SSH connections
  - `rscon_ssh_auth_failures` - authentication failures
  - `rscon_ssh_auth_success` - successful authentications
  - `rscon_ssh_authorization_success` - successful authorization decisions
  - `rscon_ssh_authorization_failure` - failed authorization decisions
  - `rscon_ssh_concurrent_sessions` - current concurrent sessions
  - `rscon_ssh_max_concurrent_sessions` - max concurrent sessions limit
  - `rscon_ssh_session_duration` - session duration histogram
  - `rscon_recording_errors` - recording errors counter
  - `rscon_audit_events_total` - total audit events
  - `rscon_audit_chain_violations` - chain violation count
  - `rscon_api_requests_total` - total API requests (by method, status, handler)
  - `rscon_api_request_duration` - request duration histogram
  - `rscon_startup_time` - startup timestamp
  - `rscon_version` - version info

- **Health endpoints**:
  - `GET /api/v1/health` - health check
  - `GET /api/v1/version` - version info

- **Logging**:
  - Structured JSON logging via `slog`
  - Configurable log level (`RSCON_LOG_LEVEL`)
  - Configurable log format (`RSCON_LOG_FORMAT`)

### Planned Implementation (future release)
When OpenTelemetry tracing support is added, the following will be implemented:

1. **Tracer Provider** (`internal/observability/otel.go`):
   - OpenTelemetry Go SDK tracer provider
   - Configured with environment variables or YAML config
   - Support for stdout/otlp exporters
   - Resource detection (service name, version, etc.)

2. **Middleware** (`internal/api/middleware.go`):
   - OpenTelemetry middleware for HTTP handlers
   - Automatic span creation for each request
   - Context propagation with traceparent header
   - Error handling with span recording

3. **Span Attributes** (key-value attributes on spans):
   - `http.method`, `http.url`, `http.status_code`
   - `db.system`, `db.connection_string` (if applicable)
   - `peer.address`, `peer.name`
   - `queue.name`, `queue.size`
   - `messaging.system`, `messaging.destination`

4. **Exporters** (`internal/observability/exporters.go`):
   - **OTLP exporter**: OpenTelemetry Protocol endpoint
   - **Jaeger exporter**: Jaeger collector
   - **Prometheus exporter**: Metrics conversion (already partially implemented)
   - **Logging exporter**: Structured log output

5. **Propagation**:
   - HTTP `traceparent` header propagation
   - gRPC metadata propagation
   - Context carryover between handler chains

### Migration Path
When OpenTelemetry tracing support is added:
1. Add otel Go dependency: `go get go.opentelemetry.io/otel/sdk`
2. Configure tracer provider in `internal/observability/`
3. Add middleware to all API handlers
4. Update web console to display trace information
5. Add roadmap status change from `pending` to `partial`
6. Add integration tests with a tracing backend

### Roadmap Status
- **Current phase**: Pending (0% complete)
- **Estimated effort**: 2-3 sprints
- **Dependencies**: SDK import, middleware wrapping, web console updates
- **Will update to `partial` status** when tracing is implemented

### References
- See `CONTRIBUTING.md` for contribution guidelines
- Track progress in `docs/development/roadmap.md`
- See `RELEASE_NOTES.md` for version planning
- OpenTelemetry Go: https://github.com/open-telemetry/opentelemetry-go
- OpenTelemetry specification: https://opentelemetry.io/docs/specs/
- Prometheus integration: https://pkg.go.dev/go.opentelemetry.io/otel/exporters/prometheus
