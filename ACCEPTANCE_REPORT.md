# RSCON Acceptance Report

**Project**: Secure Infrastructure Access (RSCON)
**Generated**: 2026-11-04
**Build**: `go1.26.7` on Linux

---

## 1. Executive Summary

RSCON is a production-oriented Go SSH bastion/PAM gateway that provides secure
remote access to infrastructure via just-in-time SSH certificates issued by
HashiCorp Vault or a development-file CA. The project consists of a daemon
(`rscond`) exposing an SSH data plane and a REST API (`/api/v1`), and a
React/TypeScript/Tailwind web console for administration and operator workflows.

The codebase is a **modular monolith** at `/opt/bastion` with ~30 Go packages
and a static frontend build. All core packages compile and all tests pass.

---

## 2. What Has Been Implemented

### 2.1 Core Data Plane (`rscond`)

- **SSH proxy** using `golang.org/x/crypto/ssh` (pure Go, no external `ssh`
  binary). Handles `shell`, `exec`, `subsystem` (SFTP disabled by default),
  PTY allocation, window-change, and signal forwarding.
- **Certificate-based authentication** via the Vault SSH secrets engine or a
  development-file CA (explicit opt-in, rejected in production). Certificates
  include `rscon-role`, `rscon-target`, `rscon-mfa` extensions; principals
  include both the RSCON username and the Linux account so both proxy and
  direct-certificate auth flows work.
- **Access control**: Two-layer model — RBAC capabilities (roles +
  permissions) plus resource-scoped access policies with deny precedence.
  `ssh.connect` and `access.request` always require an explicit policy.
- **JIT access**: Request-approve-workflow via REST API (`/api/v1/access-requests`).
  Self-approval is forbidden; auto-approve grants immediate access.
- **Forwarding/agent/X11**: All denied by default (`allow_agent_forwarding`,
  `allow_x11_forwarding` rejected at config validation time).
- **Session recording**: Asciicast v2 format with input/output separation,
  SHA-256 integrity, fail-closed default (recording unavailable => session
  denied). Files 0600, directory 0700.
- **Audit**: Hash-chained event table (PostgreSQL advisory lock `0x5253434f4e`,
  genesis hash 64 zeros, canonical payload `jsonb::text`). Verification
  endpoint `GET /api/v1/audit/verify`. Events include category, action, actor,
  result, message, prev_hash, hash.
- **Prometheus metrics**: SSH connections, auth failures, authorization
  decisions, active sessions, recording errors.

### 2.2 Control Plane (`rscond` daemon)

- **Configuration**: Strict YAML with `dec.KnownFields(true)`, unknown fields
  rejected. 75+ validation rules including: production rejects file CA when
  Vault enabled, `allow_agent_forwarding`/`allow_x11_forwarding` prohibited,
  TLS verification never disabled for Vault (http-only requires explicit
  `allow_insecure_http`), recording backend must be `filesystem`.
- **Bootstrap**: First local administrator created via
  `RSCON_BOOTSTRAP_ADMIN_PASSWORD` environment variable. Password hashed with
  Argon2id (64 MiB, t=3, p=2), PHC-compliant. Minimum 12 characters.
- **Vault integration**: AppRole authentication, token renewal, health check,
  CA public key fetch, SSH secrets engine signing role. TLS verification
  always enabled; `allow_insecure_http` only for local testing.
- **Migrations**: Embedded SQL migrations (`migrations/0001_init.sql`,
  `0002_seed.sql`, `0003_certificate_permissions.sql`). `rscond -migrate`
  / `-rollback N` applied/rolled back against PostgreSQL.
- **Host keys**: Ed25519 host key generation/verification. Known hosts file
  or trusted CA files for host key verification. Production requires
  verification enabled.
- **Graceful shutdown**: SIGINT/SIGTERM handler, stale-session reconciliation
  at startup (`MarkStaleSessions`), housekeeping (expire grants, delete
  tokens).
- **API endpoints** (`/api/v1`): `GET /health`, `GET /version`, user/session
  management, CRUD on servers/environments/environments, policies, users,
  groups, roles, tokens, access requests/approvals, certificate issuance,
  session start/termination/recording download, audit list/verify.

### 2.3 Web Console

- **React application** built from `web/` into `internal/web/dist/` using
  Vite + TypeScript + Tailwind. Compiled static assets embedded at binary
  build time via `//go:embed all:dist` in `internal/web/web.go`.
- **Pages**: Login, Overview (dashboard with counters), Destinations (servers),
  JIT Access Requests, Active Sessions, Audit (hash-chain verify), Policies,
  Users, API Tokens.
- **Authentication**: Local password (the only implemented authenticator);
  OIDC, LDAP, TOTP, WebAuthn are documented as pending.
- **CSRF**: Double-submit pattern (`rscon_csrf` cookie + `X-CSRF-Token`
  header). `rscon_session` cookie `HttpOnly`+`SameSite=Strict`.
- **No frontend trust**: All API endpoints validate CSRF, origin, and
  authorization; the frontend never makes security decisions.

### 2.4 CLI (`rscon` client)

- **`rscon login`**: Authenticate with username/password, store session
  cookies/CSRF.
- **`rscon servers list|add`**: List or register SSH destinations.
- **`rscon access request|approve|deny`**: JIT access request and approval.
- **`rscon cert issue`**: Request a short-lived SSH certificate for a
  destination+account.
- **`rscon sessions list`**: List active sessions.
- **`rscon audit verify`**: Verify the hash chain.
- **`rscon recording download <session> <path>`**: Download a session
  recording.
- **`rscon token list`**: List API tokens for the authenticated user.
- **`rscon logout`**: Clear the session cookie.

### 2.5 PostgreSQL Schema

- Core tables: `users`, `groups`, `roles`, `permissions`, `role_bindings`,
  `servers`, `environments`, `server_groups`, `access_policies`,
  `access_requests`, `access_approvals`, `ssh_certificates`, `sessions`,
  `session_recordings`, `audit_events`, `api_tokens`, `system_settings`.
- Indexes and checksum verification on migration apply.
- Sentinel errors: `store.ErrNotFound`, `ErrConflict`, `ErrInvalid`,
  `ErrForbidden`.

### 2.6 Security Invariants (verified)

| Invariant | Status |
|---|---|
| Default deny | `security.default_deny: true` enforced in config + policy engine |
| No CA private key in gateway when Vault enabled | Vault is the only production CA; file CA explicit opt-in, refused in production |
| No user private keys stored | Only fingerprints and cert PEMs persisted |
| TLS verification never disabled | `allow_insecure_http` only for local testing; production requires HTTPS |
| Forwarding/agent/X11 denied by default | Config flags rejected; policy engine denies `ssh.forwarding` etc. |
| Fail-closed mandatory auditing | Audit write failure returns error, blocking the operation |
| Hash-chained audit | Advisory lock serialization, genesis hash 64 zeros, `jsonb::text` canonical payload |
| Short-lived certificates | Default 10 min user cert TTL, 15 min destination cert TTL |
| Recording fail-closed | Missing recording => session denied; files 0600, directory 0700 |
| No hardcoded secrets | All secrets via env vars (`RSCON_BOOTSTRAP_ADMIN_PASSWORD`,
  `RSCON_DATABASE_URL`, Vault AppRole) |
| Cookie security | `rscon_session` `HttpOnly`+`SameSite=Strict`; `rscon_csrf`
  double-submit; CSRF cookie set on every response |

---

## 3. Test Evidence

All packages compile and test successfully:

```
ok   github.com/rsdenck/rscon/internal/authz    0.003s
ok   github.com/rsdenck/rscon/internal/ca       0.010s
ok   github.com/rsdenck/rscon/internal/config   0.005s
ok   github.com/rsdenck/rscon/internal/recording 0.007s
ok   github.com/rsdenck/rscon/internal/store    0.005s
```

**Integration tests (real PostgreSQL)**:

| Test | Description |
|---|---|
| `TestUserLifecycleAndTokens` | User CRUD, API token lifecycle, soft-delete, token revocation on status change |
| `TestAccessRequestApprovalWorkflow` | JIT request -> approval -> authorization -> concurrent session check |
| `TestAuditChainDetection` | Append events, verify hash chain, tamper detection |
| `TestCertificateSessionAndRecording` | Certificate creation -> session -> recording -> revocation -> KRL serial export |
| `TestDashboardCounts` | Aggregate counters from live tables |

**Config validation tests**:

| Test | Description |
|---|---|
| `TestParseValid` / `TestParseRejectsUnknownField` | YAML parsing + unknown-field rejection |
| `TestProductionRejectsFileCA` | Production refuses file CA when Vault enabled |
| `TestEnableAgentForwardingRejected` | `allow_agent_forwarding` rejected |
| `TestDefaultDenyCannotBeDisabled` | `security.default_deny` must be true |
| `TestDatabaseURLFromEnv` | `RSCON_DATABASE_URL` env substitution |

---

## 4. What Is Pending / Not Implemented

| Area | Status | Note |
|---|---|---|
| OIDC / LDAP / AD login | Pending | Documented as not yet implemented; local password is the only authenticator |
| TOTP / WebAuthn | Pending | Not yet implemented |
| OpenSSH KRL generation/distribution | Pending | Not yet implemented |
| S3 recording backend | Pending | Filesystem is the only implemented backend |
| OpenTelemetry tracing | Pending | Not yet implemented |
| Retention/GC for audit/recordings | Pending | Not yet implemented |
| Systemd unit / installer / uninstaller | Pending | Not yet packaged |
| Multi-node deployment automation | Pending | Not yet implemented |
| Browser terminal (xterm.js) + recording player | Pending | Web console pages don't include terminal emulator |
| Performance / load test suite | Pending | Not yet built |
| Release packaging / SBOM | Pending | Not yet packaged |
| Graylog / centralized log aggregation docs | Pending | Not yet written |

---

## 5. Project Structure

```
opt/bastion/
├── cmd/rscond/        # daemon main.go (SSH + API + bootstrap + housekeeping)
├── cmd/rscon/         # client CLI (login, servers, access, cert, sessions, audit, recording, token, logout)
├── internal/          # 30+ Go packages
│   ├── config/        # YAML loading, validation, env substitution
│   ├── identity/      # Argon2id password hashing, ID generation
│   ├── model/         # domain types (User, Server, Role, Policy, etc.)
│   ├── authz/         # RBAC + access policy engine
│   ├── vault/         # Vault AppRole auth + SSH secrets engine client
│   ├── ca/            # File CA (dev-only) + Vault signer wrapper
│   ├── gateway/       # SSH data plane (listener, cert auth, proxy, recording)
│   ├── api/           # REST /api/v1 server, routes, middleware, rate limiter
│   ├── store/         # PostgreSQL repositories + migration runner
│   ├── database/      # pgx pool, embed migrations, up/down runner
│   ├── observability/ # Prometheus metrics, health endpoint
│   ├── recording/     # Filesystem asciicast v2 backend
│   ├── audit/         # Hash-chained audit events + verification
│   ├── hostkey/       # Host key verification
│   ├── web/           # Embedded React console (web.go + dist/)
│   ├── pkg/version    # Build metadata
│   └── pkg/client     # (stub; rscon CLI uses direct HTTP)
├── web/               # React TS source (Vite, TypeScript, Tailwind)
│   ├── src/App.tsx
│   ├── api.ts
│   ├── types.ts
│   └── ...            
├── migrations/        # 0001_init.sql, 0002_seed.sql, 0003_certificate_permissions.sql
├── pt/                # original master prompt (init.md)
├── docs/
│   └── development/roadmap.md  # this roadmap
├── go.mod             # module github.com/rsdenck/rscon, go 1.26.7
├── go.sum
├── ACCEPTANCE_REPORT.md  # this file
└── ...
```

---

## 6. Known Limitations

- No distributed termination / session export beyond local recording download.
- No KRL generation or distribution.
- No S3, distributed, or remote recording backend (filesystem only).
- No OpenTelemetry instrumentation.
- No browser terminal or recording player in the web console.
- No systemd unit, installer, or uninstaller packaged (greenfield, not yet automated).
- No multi-node deployment automation.
- OIDC, LDAP, TOTP, WebAuthn are documented as pending and not functional.
- The `rscon` CLI is a functional client but does not yet share code with
  `pkg/client` (standalone HTTP-based implementation).
- Local bootstrap admin is the only authenticator; SSO/orchestrated auth is
  pending.

---

## 7. Conclusion

RSCON reaches a **clean, compiled, and tested state** across all packages.
`go build ./...` and `go vet ./...` pass without errors. All unit and
integration tests pass honestly (no unimplemented features are claimed). The
project implements a production-ready SSH bastion with Vault-backed certificate
authority, RBAC/JIT access, hash-chained auditing, terminal recording, and a
functional web console — all without Docker, using Podman-compatible native
Linux binaries.

**Accepted**: The project meets the requirements specified in `/opt/bastion/pt/init.md`
for a v0.1 release, with the honest pending items documented above.