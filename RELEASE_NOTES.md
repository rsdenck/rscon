# RSCON v0.1.0 Release Notes

## Release Date
2026-11-04

## Summary
RSCON (Secure Infrastructure Access) is a production-oriented Go SSH bastion/PAM gateway providing secure remote access to infrastructure via just-in-time SSH certificates issued by HashiCorp Vault or a development-file CA.

## What's New in v0.1.0

### Core Data Plane (rscond)
- SSH proxy implemented in pure Go using `golang.org/x/crypto/ssh` (no external `ssh` binary)
- Certificate-based authentication via Vault SSH secrets engine or development-file CA
- JIT access workflow: request → approve → authorize → connect
- Session recording in Asciicast v2 format with SHA-256 integrity
- Hash-chained audit table with verification endpoint
- Default-deny channel policy (agent forwarding, X11 forwarding, TCP forwarding all disabled by default)
- Session lifecycle: idle/max duration limits, concurrent session limits, graceful shutdown
- PTY, terminal resize, shell and exec channels
- Pipes created before destination Start (fixes race condition)
- Destination request forwarding for window-change and signal

### Control Plane
- Strict YAML configuration with unknown-field rejection (`dec.KnownFields(true)`)
- Environment variable substitution for `database.url`, `vault.address`
- 75+ validation rules enforced at startup (`rscond -check`)
- Embedded SQL migrations with up/down support (`rscond -migrate`, `-rollback N`)
- Bootstrap administrator with Argon2id hashing (min 12 chars, `RSCON_BOOTSTRAP_ADMIN_PASSWORD`)
- Vault AppRole authentication, health check, token renewal, CA public key fetch
- File CA explicit opt-in (`dev.insecure_file_ca`), refused in production
- Prometheus metrics and health endpoints
- Syslog/GELF fan-out for audit events

### Web Console
- React application built from `web/` into `internal/web/dist/` using Vite + TypeScript + Tailwind
- Embedded at binary build time via `//go:embed all:dist` in `internal/web/web.go`
- Pages: Login, Overview (dashboard), Destinations (servers), JIT Access Requests, Active Sessions, Audit (hash-chain verify), Policies, Users, API Tokens
- Local password authentication (only implemented authenticator; OIDC/LDAP/TOTP/WebAuthn documented as pending)
- CSRF double-submit pattern (`rscon_csrf` cookie + `X-CSRF-Token` header)
- `rscon_session` cookie `HttpOnly`+`SameSite=Strict`
- No frontend trust: all API endpoints validate CSRF, origin, and authorization

### CLI (`rscon`)
- `rscon login <username> <password>` - authenticate and store session
- `rscon servers list|add <name> <hostname> <address> [port] [environment_id] [default_account]` - manage destinations
- `rscon access request <server> <account> [reason] [minutes]` - request JIT access
- `rscon access approve|deny <request_id> [comment]` - approve/deny access requests
- `rscon cert issue <server> <account> [ticket] [minutes]` - obtain short-lived SSH certificate
- `rscon sessions list` - list active sessions
- `rscon audit verify` - verify hash chain
- `rscon recording download <session_id> <output_path>` - download session recording
- `rscon token list` - list API tokens for authenticated user
- `rscon logout` - clear session

### Security Invariants (enforced)
- Default deny: `security.default_deny: true` cannot be disabled
- No CA private key in gateway when Vault enabled (Vault is production-only CA)
- No user private keys stored (only fingerprints and cert PEMs persisted)
- TLS verification never disabled for Vault connections (`allow_insecure_http` only for 127.0.0.1 local testing)
- Forwarding/agent/X11 denied by default; policy-controlled opt-in
- Fail-closed mandatory auditing: audit write failure blocks the operation
- Hash-chained audit: advisory lock `0x5253434f4e`, genesis hash 64 zeros, canonical `jsonb::text`
- Short-lived certificates: default 10 min user cert TTL, 15 min destination cert TTL
- Recording fail-closed: missing recording => session denied; files 0600, directory 0700
- No hardcoded secrets: all secrets via environment variables
- Cookie security: `rscon_session` `HttpOnly`+`SameSite=Strict`; `rscon_csrf` double-submit

### Packaging
- Native Linux binaries (`rscond`, `rscon`) compiled with Go 1.26.7
- No Docker; Podman rootless noted as compatible; Docker forbidden per requirements
- Configuration at `/etc/rscon/config.yaml`
- Data at `/var/lib/rscon/`
- Logs at `/var/log/rscon/`
- Systemd unit available (sample provided)
- Docker/Podman image: `ghcr.io/rsdenck/rscon`

### Testing
- `go build ./...` passes cleanly
- `go vet ./...` passes cleanly
- `go test -count=1 ./...` all 5 packages pass
- 5 integration tests with real PostgreSQL (user lifecycle, access workflow, audit chain, cert+session+recording, dashboard counts)
- Configuration validation tested (`rscond -check`)
- Unknown YAML field rejection verified

## Upgrading from Previous Versions
- v0.1.0 is the initial release
- Migration `0003_certificate_permissions` grants `certificate.issue`/`certificate.read` to platform/Linux/contractor/infrastructure roles
- Existing databases must have migration `0003` applied: `rscond -migrate`
- Update configuration to match new v0.1.0 defaults

## Known Limitations (v0.1.0)
- OIDC/LDAP/TOTP/WebAuthn login: documented as pending; local password is the only authenticator
- S3 recording backend: filesystem is the only implemented backend
- OpenTelemetry tracing: not yet implemented
- Browser terminal (xterm.js) and recording player: not yet implemented
- Systemd unit / installer / uninstaller: not yet packaged
- Multi-node deployment automation: not yet implemented
- Performance/load test suite: not yet built
- KRL generation and distribution: not yet implemented
- Browser terminal/recording player in web console: not yet implemented

## Contributing
See `CONTRIBUTING.md` for development workflow, coding standards, and release process.

## Contact
Report security issues via private email. See `SECURITY.md` for the full security policy.

---
*RSCON v0.1.0 - Secure Infrastructure Access*
