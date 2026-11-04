# RSCON Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [0.1.0-2026.11.04] - 2026-11-04

### Added
- **Web console**: React application embedded at build time via `//go:embed`, serving login, overview, destinations, JIT access, sessions, audit, policies, and tokens pages
- **Migration `0003`**: Grants `certificate.issue`/`certificate.read` to platform/Linux/contractor/infrastructure roles
- **Integration tests**: 5 PostgreSQL-backed tests (user lifecycle, access workflow, audit chain, cert+session+recording, dashboard counts)
- **Config validation**: 75+ rules including production file-CA rejection, forwarding/X11 denial, TLS enforcement
- **Audit hash chain**: verification endpoint `GET /api/v1/audit/verify`, advisory lock serialization, genesis hash 64 zeros
- **Recording**: Asciicast v2 format with SHA-256 integrity, fail-closed default
- **CLI `rscon`**: login, servers, access request/approve/deny, cert issue, sessions list, audit verify, recording download, token list, logout
- **Bootstrap admin**: Argon2idhashed password, min 12 chars, `RSCON_BOOTSTRAP_ADMIN_PASSWORD`
- **Vault integration**: AppRole auth, TLS-verified CA, health check, token renewal
- **File CA**: explicit opt-in via `dev.insecure_file_ca`, refused in production
- **Channel policy**: agent forwarding, X11 forwarding, TCP forwarding all denied by default
- **Roadmap documentation**: `docs/development/roadmap.md` with done/partial/pending status
- **Acceptance report**: `ACCEPTANCE_REPORT.md` with full implementation summary

### Changed
- **Configuration**: unknown YAML fields rejected (`dec.KnownFields(true)`); env var substitution for `database.url`, `vault.address`
- **Web console**: TS compilation errors fixed (removed unused `useMemo`); Vite build outputs to `internal/web/dist/`
- **Store**: added `normalisePolicy()` to prevent NULL array/jsonb columns; added `GetUser`/`GetUserByKeyFingerprint` convenience methods
- **Gateway**: pipes created before destination `Start`; `window-change`/`signal` request forwarding; `time.AfterFunc` replaces flawed max-duration goroutine
- **Acceptance test evidence**: all packages `go test -count=1 ./...` pass; integration tests with real PostgreSQL pass

### Fixed
- **TS error**: removed unused `useMemo` import from `App.tsx` (error TS6133)
- **Build**: `go build ./...` and `go vet ./...` clean across all packages
- **Store nulls**: `normalisePolicy()` converts nil Go slices to empty slices before SQL INSERT
- **Migrate**: migration `0003_certificate_permissions` applied/rolled back correctly

### Security
- **Default deny**: `security.default_deny: true` enforced in config + policy engine
- **No CA key in gateway**: Vault-only in production; file CA explicit opt-in, refused in production
- **TLS never disabled**: `allow_insecure_http` only for 127.0.0.1 local testing
- **Forwarding denied**: `allow_agent_forwarding`/`allow_x11_forwarding`/`allow_tcp_forwarding` rejected at config validation
- **Fail-closed auditing**: audit write failure returns error, blocking the operation
- **Hash-chained audit**: advisory lock `0x5253434f4e`, genesis hash 64 zeros, canonical `jsonb::text`
- **No private keys stored**: only fingerprints and cert PEMs persisted
- **Cookie security**: `rscon_session` `HttpOnly`+`SameSite=Strict`; `rscon_csrf` double-submit

## [0.1.0-2026.11.03] - 2026-11-03

### Added
- Initial prototype setup: Go module `github.com/rsdenck/rscon`, Go 1.26.7
- Phase 0 architecture, threat model, security model, roadmap
- Core packages: `pkg/version`, `identity`, `config`, `model`, `authz`, `database`, `store`, `vault`, `ca`, `recording`, `audit`, `observability`, `gateway`, `api`, `web`
- Basic SSH proxy and certificate authentication
- Minimal CLI scaffolding

### Fixed
- Module initialization and dependency resolution
- go.mod drift fixes (prometheus, term, puddle packages)

### Deprecated
- None (v0.1.0-initial)
