# Minha recomendação final:
Componente	Tecnologia
Linguagem principal	Go
Protocolo SSH	golang.org/x/crypto/ssh
Gerenciamento de segredos	HashiCorp Vault
Identidade SSH	SSH CA e certificados temporários
Autenticação corporativa	OIDC, com MFA no provedor
Banco de dados	PostgreSQL
Console web	API REST + frontend independente
Auditoria	Graylog via syslog ou integração HTTP
Implantação	Linux + systemd; containers quando apropriado
Testes de segurança	Testes de integração SSH, testes de autorização e análise de dependências
-----------------------------------------------------------------
# PROIBIDO USA DOCKER!
# USAR PODMAN COM ROOTLESS
# MASTER PROMPT — Enterprise SSH Bastion & Privileged Access Gateway

## 1. ROLE AND MISSION

Act as a Principal Software Architect, Senior Golang Engineer, Linux Security Engineer, SSH Protocol Engineer, and Privileged Access Management (PAM) Specialist.

Your mission is to design and develop a complete, production-oriented, open-source SSH Bastion platform written primarily in **Go (Golang)**.

The product must be an independent implementation, with its own architecture, source code, APIs, configuration system, CLI, security model, and documentation.

It must not be a simple SSH wrapper, shell script, SSH configuration generator, or frontend around another commercial product.

Build a genuine SSH access gateway capable of securely managing privileged access to Linux infrastructure.

Use established open-source projects as architectural references:

- Teleport: https://github.com/gravitational/teleport
- The Bastion: https://github.com/ovh/the-bastion
- JumpServer: https://github.com/jumpserver/jumpserver
- Bastillion: https://github.com/Loophole-LLC/Bastillion
- sshportal: https://github.com/moul/sshportal
- HashiCorp Vault: https://github.com/hashicorp/vault

Study their architectures, concepts, protocols, security mechanisms, and relevant documentation.

Do not copy their proprietary code, trademarks, or licensed implementations. Develop an original implementation.

### Primary objective

Create a centralized SSH access platform that allows administrators and authorized engineers to access Linux servers through a controlled gateway.

The platform must provide:

- Centralized SSH access.
- User and identity management.
- SSH Certificate Authority (SSH CA).
- HashiCorp Vault integration.
- Temporary SSH certificates.
- Role-Based Access Control (RBAC).
- Multi-Factor Authentication (MFA).
- Just-In-Time (JIT) access.
- Approval workflows.
- SSH session recording and auditing.
- Command and channel restrictions.
- Centralized logging.
- Web administration interface.
- REST API.
- CLI.
- High availability.
- Enterprise-grade security.

The platform should be modular, scalable, maintainable, secure, and easy to deploy on Linux.

---

# 2. TECHNOLOGY STACK

Use the following technology stack as the architectural baseline.

| Component | Technology |
|---|---|
| Main language | Go |
| SSH protocol | golang.org/x/crypto/ssh |
| SSH certificate management | OpenSSH SSH CA |
| Secrets management | HashiCorp Vault |
| Primary database | PostgreSQL |
| Database migrations | golang-migrate or equivalent |
| Cache / ephemeral coordination | Redis, only when justified |
| Authentication | OIDC / OAuth2 |
| MFA | OIDC provider MFA and optional TOTP |
| Authorization | Native RBAC and policy engine |
| Backend API | Go REST API |
| API documentation | OpenAPI |
| Frontend | TypeScript + React |
| Frontend styling | Tailwind CSS |
| Terminal interface | xterm.js |
| Metrics | Prometheus |
| Observability | OpenTelemetry |
| Logging | Structured JSON |
| Log aggregation | Graylog / Syslog / HTTP |
| Deployment | Linux + systemd |
| Container support | Docker / Podman |
| Testing | Go testing, integration tests, security tests |
| CI/CD | GitHub Actions |

Do not introduce additional technologies without a technical justification.

Prefer standard Go libraries and established, maintained dependencies.

Avoid unnecessary microservices. Begin with a modular monolith, keeping clear boundaries between the control plane and data plane so they can be separated in the future.

---

# 3. ARCHITECTURAL PRINCIPLES

Follow these principles throughout development:

1. Security by design.
2. Least privilege.
3. Zero Trust access model.
4. Default deny.
5. Short-lived credentials.
6. No shared permanent SSH credentials.
7. No hardcoded secrets.
8. No insecure cryptographic implementations.
9. Explicit authorization before establishing connections.
10. Complete auditability of privileged operations.
11. Separation of duties.
12. Secure failure behavior.
13. Backward-compatible configuration migrations.
14. Automated testing for critical security decisions.

The gateway must never trust a user merely because they can reach its SSH port.

Every connection must be authenticated, authorized, and associated with a traceable identity.

---

# 4. PROJECT ARCHITECTURE

Implement the platform using the following logical components.

## 4.1 SSH Gateway

The SSH Gateway is the core of the product.

Responsibilities:

- Accept incoming SSH connections.
- Implement SSH server authentication.
- Identify users and their credentials.
- Validate certificates.
- Evaluate authorization policies.
- Resolve destination servers.
- Establish outgoing SSH connections.
- Proxy interactive sessions.
- Handle terminal resizing.
- Handle SSH channels.
- Enforce forwarding policies.
- Enforce session limits.
- Register session metadata.
- Start and stop session recording.
- Terminate unauthorized connections.

Use Go SSH libraries.

Implement a real SSH proxy, not an implementation that simply executes an external ssh command for every connection.

Support:

- Interactive shell.
- PTY allocation.
- Terminal resizing.
- Command execution.
- SFTP, controlled by policy.
- Local and remote port forwarding, controlled by policy.
- SCP compatibility where technically supported.
- Connection timeouts.
- Idle session timeouts.
- Concurrent sessions.

Forwarding, agent forwarding, X11 forwarding, and other potentially dangerous channels must be denied by default.

Do not expose unrestricted shell access to the bastion host itself.

The gateway must only provide explicitly authorized access to registered destinations.

## 4.2 Control Plane

Implement a centralized control plane responsible for:

- Users.
- Groups.
- Roles.
- Permissions.
- Server inventory.
- Credential policies.
- Authentication configuration.
- Approval workflows.
- Access requests.
- Session metadata.
- Audit events.
- System configuration.
- Administrative operations.

The control plane must expose a documented REST API.

## 4.3 Data Plane

Keep the SSH traffic processing layer logically independent.

The data plane must:

- Receive connections.
- Authenticate identities.
- Enforce access decisions.
- Proxy SSH traffic.
- Apply connection policies.
- Generate session events.
- Communicate with the control plane securely.

Design for future deployment of multiple gateway instances.

## 4.4 Database

Use PostgreSQL.

Create a normalized database schema with migrations.

Required entities:

- users
- groups
- roles
- permissions
- user_groups
- role_bindings
- servers
- server_groups
- environments
- access_policies
- access_requests
- access_approvals
- sessions
- session_recordings
- ssh_certificates
- authentication_events
- audit_events
- system_settings
- api_tokens
- identity_providers

Add appropriate:

- Foreign keys.
- Indexes.
- Unique constraints.
- Timestamps.
- Soft deletion where appropriate.
- Referential integrity.
- Transactional operations.

Never store private SSH keys in PostgreSQL.

Do not store raw passwords, MFA secrets in plaintext, Vault tokens in plaintext, or sensitive authentication material in logs.

Implement database migrations and rollback procedures.

---

# 5. SSH CERTIFICATE AUTHORITY — SSH CA

This is one of the most important parts of the platform.

Implement SSH CA integration using OpenSSH-compatible certificates.

The platform must support:

- User SSH certificates.
- Host SSH certificates.
- Certificate signing.
- Certificate validation.
- Certificate expiration.
- Certificate principal restrictions.
- Certificate identity mapping.
- Certificate revocation.
- Trusted CA management.
- Certificate issuance audit records.

## 5.1 HashiCorp Vault integration

Integrate with the Vault SSH secrets engine.

Documentation:

https://developer.hashicorp.com/vault/docs/secrets/ssh

Support the appropriate Vault workflows, including:

- SSH CA signing.
- Signed user certificates.
- Configurable signing roles.
- TTL restrictions.
- Principal restrictions.
- Allowed extensions.
- Certificate validity constraints.
- CA public key distribution.
- Vault health checks.
- Vault authentication.
- Vault token lifecycle management.

Use Vault's signing capabilities rather than implementing your own cryptographic signing algorithm.

Important security requirements:

- The CA private key must never be exposed to the SSH gateway.
- Prefer storing and protecting the CA private key inside Vault.
- The user's private key must remain under the user's control whenever possible.
- Send only the public key to Vault for certificate signing.
- Never request or store users' private keys unnecessarily.
- Never place Vault root tokens in application configuration.
- Prefer workload identity or an appropriately configured AppRole authentication flow.
- Apply least privilege to Vault policies.
- Do not disable TLS certificate verification.

Implement a Vault provider interface so other supported CA backends can be added in the future.

## 5.2 Certificate issuance workflow

Implement the following process:

1. User authenticates to the platform.
2. Platform validates identity.
3. Platform evaluates user permissions.
4. Platform validates destination and environment.
5. Platform checks whether approval is required.
6. User supplies a public SSH key or generates a key locally.
7. Platform requests certificate signing from Vault.
8. Vault signs the public key.
9. Platform returns the signed SSH certificate.
10. User authenticates against the destination using the certificate.
11. The destination validates the certificate against the trusted SSH CA.
12. Platform records the issuance and access events.

Certificates must have configurable short lifetimes.

Use conservative defaults, such as 10 minutes for interactive administrative access, configurable by policy.

Do not use a permanent SSH certificate as a substitute for identity verification.

## 5.3 Revocation

Implement:

- Certificate revocation records.
- OpenSSH Key Revocation Lists (KRL), where applicable.
- Revocation distribution.
- Emergency identity disablement.
- Session termination after an account is disabled.
- Revocation audit records.

Remember that certificate expiration and revocation are different mechanisms.

Do not claim that a certificate can be revoked merely by deleting its database record.

Implement and document how destinations receive and enforce revocation information.

---

# 6. AUTHENTICATION AND IDENTITY

Implement a flexible authentication architecture.

Supported authentication methods:

1. OIDC.
2. OAuth2.
3. LDAP.
4. Active Directory.
5. Local administrative accounts.
6. SSH public-key identity.
7. TOTP MFA, when required.
8. WebAuthn/FIDO2 through a supported identity provider or implementation.

Prioritize OIDC for the initial release.

Allow integration with:

- Keycloak.
- Microsoft Entra ID.
- Google Workspace.
- Other compatible OIDC providers.

Implement:

- User provisioning.
- User deactivation.
- Group synchronization.
- Identity mapping.
- Session expiration.
- Login audit events.
- MFA enforcement.
- Brute-force protection.
- Rate limiting.
- Account lockout policies.

Never use a local account to bypass external authentication policies.

Local administrative access must have a documented recovery procedure.

---

# 7. AUTHORIZATION AND RBAC

Implement a complete RBAC model.

Example roles:

- Super Administrator.
- Security Administrator.
- Infrastructure Administrator.
- Platform Engineer.
- Linux Administrator.
- Read-Only Auditor.
- Operator.
- Temporary Contractor.

Authorization must support multiple dimensions:

- User.
- Group.
- Role.
- Destination.
- Destination group.
- Environment.
- Linux account.
- Authentication strength.
- Time window.
- Access duration.
- Approval requirement.
- Allowed protocols.
- Allowed SSH channels.

Example:

A Platform Engineer may access development servers using an authorized Linux account.

A Linux Administrator may access production servers only after MFA and additional approval.

An Auditor may inspect audit events and session metadata but must not establish SSH connections.

Implement explicit deny rules that take precedence over allow rules.

Never rely exclusively on frontend permissions. All authorization must be enforced by the backend and SSH gateway.

---

# 8. JUST-IN-TIME ACCESS AND APPROVALS

Implement temporary privileged access.

Users must be able to request access to:

- Individual servers.
- Server groups.
- Specific environments.
- Specific Linux accounts.

An access request should include:

- Requesting user.
- Destination.
- Requested account.
- Reason.
- Ticket/reference identifier.
- Requested duration.
- Requested permissions.
- Creation timestamp.

Approval workflow:

1. User requests access.
2. Policy engine determines approval requirements.
3. Authorized approver receives the request.
4. Approver approves or rejects.
5. Approval is recorded.
6. Temporary permission becomes active.
7. SSH certificate is issued according to the resulting authorization.
8. Permission expires automatically.
9. All actions are audited.

Implement:

- Multiple approvers where necessary.
- Separation of duties.
- Approval expiration.
- Automatic expiration of temporary grants.
- Emergency access workflows.
- Emergency access notifications.

Do not allow users to approve their own requests when separation of duties is required.

---

# 9. SESSION RECORDING AND AUDIT

Implement enterprise-oriented SSH session recording.

Record:

- Session identifier.
- User identity.
- Source IP.
- Destination host.
- Destination Linux account.
- Authentication method.
- Certificate serial number.
- Start timestamp.
- End timestamp.
- Duration.
- Session termination reason.
- Exit status, when available.
- Relevant SSH channel events.
- Session recording reference.

## 9.1 Terminal recording

Capture terminal input/output events and timestamps for supported interactive sessions.

Support:

- Recording.
- Session replay.
- Search by user.
- Search by destination.
- Search by time.
- Session metadata.
- Access-controlled playback.

Do not assume terminal recording captures all activity performed by a user or all commands executed by child processes.

Clearly distinguish:

- Terminal input.
- Terminal output.
- SSH protocol events.
- Session metadata.
- Host-level activity.

Never log:

- Private keys.
- Passwords.
- Vault tokens.
- Authentication secrets.
- Sensitive application credentials.

Implement configurable masking or exclusion mechanisms where possible.

## 9.2 Recording storage

Use a storage abstraction.

Initial options:

- Local filesystem.
- S3-compatible object storage.

Design for future integration with:

- MinIO.
- AWS S3.
- IBM object storage.
- Other S3-compatible backends.

Implement:

- Recording integrity verification.
- Cryptographic hashes.
- Retention policies.
- Access control.
- Tamper-evident audit references.
- Optional object-lock or immutable storage integration.

Do not claim immutability unless the storage backend actually enforces it.

---

# 10. CENTRALIZED LOGGING AND OBSERVABILITY

The platform must have first-class logging integration.

Support:

- Structured JSON logs.
- Syslog.
- Graylog-compatible GELF or supported HTTP ingestion.
- OpenTelemetry.
- Prometheus metrics.

Create distinct log categories:

- Authentication.
- Authorization.
- SSH connections.
- SSH sessions.
- Certificate issuance.
- Certificate revocation.
- Administrative actions.
- Access approvals.
- Vault integration.
- System operations.
- Security events.

Every security-sensitive event must include:

- Timestamp.
- Event identifier.
- Actor identity.
- Action.
- Result.
- Relevant resource.
- Source address where available.
- Correlation identifier.

Provide metrics for:

- Active sessions.
- Total connections.
- Failed authentication.
- Denied authorization.
- Certificate issuance.
- Vault request failures.
- Gateway health.
- Session recording failures.
- API latency.

Implement health and readiness endpoints.

Ensure that failure to write audit events cannot silently result in a successful privileged operation when the configured policy requires mandatory auditing.

---

# 11. SERVER INVENTORY

Implement centralized destination management.

Each registered server should support:

- Hostname.
- FQDN.
- IP address.
- SSH port.
- Environment.
- Location or region.
- Operating system metadata.
- Server group.
- Tags.
- Enabled/disabled status.
- Allowed Linux accounts.
- SSH host identity.
- Host certificate validation.
- Access policy association.

Support environments:

- Development.
- Testing.
- Staging.
- Production.
- Infrastructure.
- Security.
- Disaster Recovery.

Implement server registration through:

- REST API.
- CLI.
- Administrative interface.
- Import from structured YAML/JSON.

Do not automatically trust host keys received during an unverified connection.

Implement secure host key verification, preferably using SSH host certificates signed by a trusted host CA or explicitly enrolled host keys.

---

# 12. COMMAND AND SESSION CONTROLS

Implement granular session controls.

Support:

- Shell access restrictions.
- Command execution policies.
- Linux account restrictions.
- SFTP enablement/disablement.
- SCP compatibility controls.
- TCP forwarding restrictions.
- Agent forwarding restrictions.
- X11 forwarding restrictions.
- Session idle timeout.
- Maximum session duration.
- Concurrent session limits.
- Emergency session termination.

Important:

Do not claim that filtering terminal input strings provides reliable command-level security.

Where strong command restrictions are required, use appropriate Linux operating system enforcement mechanisms, dedicated restricted accounts, forced commands, or other independently enforceable controls.

The SSH gateway must not rely on shell command parsing as its only security boundary.

---

# 13. CLI

Develop a native CLI named after the final product.

The CLI must support:

- Login.
- Logout.
- Status.
- List users.
- List servers.
- List server groups.
- Request access.
- Approve access.
- Deny access.
- Connect to server.
- Show sessions.
- Show session details.
- List certificates.
- Revoke certificates.
- Audit search.
- Configuration validation.
- Diagnostics.

Example conceptual commands:

```bash
bastion login
bastion status
bastion servers list
bastion servers show server01
bastion access request server01 --reason "Production maintenance"
bastion access list
bastion connect server01
bastion sessions list
bastion sessions show SESSION_ID
bastion sessions terminate SESSION_ID
bastion certificates list
bastion certificates revoke CERTIFICATE_ID
bastion audit search --user administrator
bastion doctor
```

The CLI must have:

- Consistent command structure.
- Helpful errors.
- Shell completion.
- JSON output.
- Human-readable output.
- Configuration profiles.
- Secure token handling.

Never print sensitive tokens or private key material.

---

# 14. WEB ADMINISTRATION INTERFACE

Build an administrative interface.

Use React, TypeScript, and Tailwind CSS.

The interface must provide:

## Dashboard

- Active SSH sessions.
- Registered servers.
- Registered users.
- Failed logins.
- Pending approvals.
- Recent security events.
- Vault status.
- Gateway health.

## User management

- User listing.
- User details.
- Groups.
- Roles.
- Permissions.
- Account status.
- Identity provider information.

## Server management

- Server inventory.
- Server groups.
- Environments.
- Tags.
- SSH configuration.
- Host identity.
- Access policies.

## Session management

- Active sessions.
- Session details.
- Session termination.
- Recording access.
- Session replay.

## Access requests

- Pending requests.
- Approval.
- Rejection.
- Request history.
- Temporary access status.

## Security administration

- Vault integration.
- SSH CA configuration.
- Certificate lifecycle.
- Trusted host CAs.
- Authentication configuration.
- MFA policies.
- RBAC policies.
- Audit configuration.

The frontend must never contain secrets or directly communicate with Vault using privileged credentials.

All security decisions must be performed server-side.

---

# 15. REST API

Design a versioned REST API.

Use:

```text
/api/v1/
```

Implement endpoints for:

```text
/api/v1/auth
/api/v1/users
/api/v1/groups
/api/v1/roles
/api/v1/permissions
/api/v1/servers
/api/v1/server-groups
/api/v1/environments
/api/v1/access-policies
/api/v1/access-requests
/api/v1/approvals
/api/v1/certificates
/api/v1/sessions
/api/v1/recordings
/api/v1/audit
/api/v1/vault
/api/v1/health
/api/v1/metrics
```

Provide:

- OpenAPI specification.
- Request validation.
- Response schemas.
- Pagination.
- Filtering.
- Sorting.
- Rate limiting.
- Authentication middleware.
- Authorization middleware.
- Consistent error responses.
- API versioning.

Protect administrative endpoints with appropriate permissions.

---

# 16. HIGH AVAILABILITY AND SCALABILITY

Design the architecture for multiple gateway instances.

Support:

- Multiple SSH gateways.
- Shared PostgreSQL.
- Vault HA integration.
- Shared recording storage.
- Centralized session metadata.
- Load balancing.
- Health checks.
- Graceful shutdown.
- Connection draining.
- Database connection pooling.

Active SSH sessions must not silently disappear from the audit trail when a gateway restarts or fails.

Document the limitations of active TCP sessions during gateway failures.

Implement reliable cleanup and reconciliation of sessions that were interrupted unexpectedly.

Do not introduce distributed coordination infrastructure without demonstrating why it is required.

---

# 17. LINUX INSTALLATION AND OPERATIONS

The product must support modern Linux distributions.

Prioritize:

- Debian.
- Ubuntu Server.
- Rocky Linux.
- AlmaLinux.
- RHEL-compatible distributions.

Implement:

- Native Linux binary.
- systemd service.
- Dedicated non-root service user.
- Restricted filesystem permissions.
- Configuration directory.
- Log directory.
- Recording directory.
- Database migrations.
- Installation script.
- Upgrade procedure.
- Uninstallation procedure.
- Configuration validation.
- Health checks.
- Backup and restore procedures.

Example directory structure:

```text
/etc/bastion/
/var/lib/bastion/
/var/log/bastion/
/var/lib/bastion/recordings/
/usr/bin/bastion
/usr/lib/systemd/system/bastion.service
```

The SSH gateway must run with the minimum privileges needed.

Do not run the application as root simply because it listens on port 22. Document and implement an appropriate privileged-port strategy.

Never weaken SSH server security settings to make the application work.

---

# 18. SECURITY REQUIREMENTS

Security is a mandatory acceptance criterion.

Implement and test:

- TLS certificate validation.
- Secure secret management.
- Authentication rate limiting.
- Brute-force protection.
- Session expiration.
- CSRF protection where applicable.
- Secure cookies.
- Strict CORS policies.
- Input validation.
- SQL injection prevention.
- Command injection prevention.
- Path traversal protection.
- SSRF protection.
- Authorization checks.
- Audit integrity verification.
- Dependency vulnerability scanning.
- Secure configuration defaults.
- Graceful handling of Vault outages.
- Secure shutdown.
- Protection against privilege escalation.

Review the OWASP ASVS and relevant OWASP API Security Top 10 guidance.

Use Go security best practices.

Do not:

- Execute untrusted shell commands.
- Interpolate untrusted input into shell commands.
- Store private keys in the database.
- Hardcode credentials.
- Disable TLS verification.
- Trust unverified SSH host keys.
- Expose Vault root credentials.
- Allow unrestricted SSH forwarding by default.
- Create universal administrative credentials.
- Suppress security errors.
- Implement cryptographic algorithms from scratch.

Add threat modeling and document the security boundaries.

---

# 19. TESTING AND QUALITY ASSURANCE

Testing is mandatory.

Create:

## Unit tests

- Authentication.
- Authorization.
- RBAC.
- Policy evaluation.
- Certificate validation.
- Certificate expiration.
- Access request workflows.
- Session state management.
- Vault provider.
- Input validation.

## Integration tests

- SSH server and client.
- SSH certificate issuance.
- Vault integration.
- PostgreSQL persistence.
- OIDC authentication.
- Session recording.
- SFTP restrictions.
- Port forwarding restrictions.
- API authentication.
- Authorization enforcement.

## Security tests

- Unauthorized SSH connection.
- Expired certificate.
- Invalid certificate.
- Revoked certificate.
- Disabled user.
- Invalid principal.
- Unauthorized destination.
- Unauthorized Linux account.
- Privilege escalation attempts.
- Authentication brute force.
- Access approval bypass.
- Session recording failure.
- Host key verification failure.

## Quality checks

Run:

```bash
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
```

Also configure:

- golangci-lint.
- govulncheck.
- Dependency scanning.
- Static analysis.
- Secret scanning.
- Frontend linting.
- Frontend tests.
- Build verification.

Never claim a test passed unless it was actually executed and its result verified.

---

# 20. REPOSITORY STRUCTURE

Use a clean, maintainable repository.

Suggested structure:

```text
.
├── cmd/
│   ├── bastion/
│   └── bastionctl/
│
├── internal/
│   ├── auth/
│   ├── authorization/
│   ├── rbac/
│   ├── policy/
│   ├── ssh/
│   │   ├── gateway/
│   │   ├── proxy/
│   │   ├── certificate/
│   │   ├── hostkey/
│   │   └── forwarding/
│   ├── vault/
│   ├── identity/
│   ├── users/
│   ├── groups/
│   ├── servers/
│   ├── approvals/
│   ├── sessions/
│   ├── recording/
│   ├── audit/
│   ├── database/
│   ├── api/
│   ├── observability/
│   ├── config/
│   └── security/
│
├── pkg/
│   ├── client/
│   ├── sdk/
│   └── version/
│
├── migrations/
│
├── web/
│   ├── src/
│   ├── public/
│   └── package.json
│
├── configs/
│   ├── bastion.example.yaml
│   └── policies/
│
├── deployments/
│   ├── systemd/
│   ├── docker/
│   └── podman/
│
├── scripts/
│
├── docs/
│   ├── architecture/
│   ├── security/
│   ├── installation/
│   ├── configuration/
│   ├── vault/
│   ├── api/
│   ├── operations/
│   └── development/
│
├── tests/
│   ├── integration/
│   ├── security/
│   └── e2e/
│
├── .github/
│   └── workflows/
│
├── Makefile
├── go.mod
├── go.sum
├── LICENSE
├── README.md
├── SECURITY.md
├── CONTRIBUTING.md
├── CODE_OF_CONDUCT.md
└── CHANGELOG.md
```

Adapt the structure when justified by the actual implementation.

Avoid creating empty packages, placeholder modules, and unnecessary abstractions.

---

# 21. CONFIGURATION

Use YAML configuration.

Create a fully documented example configuration.

Conceptual example:

```yaml
server:
  name: bastion01
  listen_address: "0.0.0.0:2222"
  api_address: "127.0.0.1:8080"

database:
  driver: postgres
  host: localhost
  port: 5432
  database: bastion
  username: bastion

vault:
  enabled: true
  address: "https://vault.example.com"
  ssh_mount: "ssh-client-signer"
  signing_role: "bastion-users"
  auth_method: "approle"

authentication:
  oidc:
    enabled: true
    issuer_url: "https://keycloak.example.com/realms/infrastructure"
    client_id: "bastion"

ssh:
  certificate_ttl: "10m"
  max_session_duration: "8h"
  idle_timeout: "30m"
  allow_agent_forwarding: false
  allow_x11_forwarding: false
  allow_tcp_forwarding: false

recording:
  enabled: true
  backend: filesystem
  directory: "/var/lib/bastion/recordings"

audit:
  enabled: true
  backend: json
  syslog:
    enabled: false

security:
  require_mfa: true
  default_deny: true
  require_host_verification: true
```

This is an illustrative configuration, not a final schema.

Implement strict configuration validation.

Do not put actual passwords, tokens, private keys, or Vault credentials in the YAML file.

Use environment variables or a dedicated secret provider.

Provide an example `.env.example` containing only non-sensitive placeholders.

---

# 22. DEVELOPMENT PHASES

Do not attempt to implement the entire platform in one uncontrolled code generation operation.

Use the following phases.

## PHASE 0 — Repository analysis and architecture

- Inspect the repository.
- Identify existing code and conventions.
- Check Git status.
- Identify existing functionality.
- Define the product architecture.
- Define module boundaries.
- Define the threat model.
- Define database entities.
- Define API contracts.
- Produce the implementation roadmap.

Deliverables:

```text
docs/architecture/overview.md
docs/architecture/components.md
docs/architecture/data-flow.md
docs/security/threat-model.md
docs/security/security-model.md
docs/development/roadmap.md
```

Do not overwrite existing user work.

## PHASE 1 — Core foundation

Implement:

- Go module.
- Configuration management.
- Structured logging.
- PostgreSQL connection.
- Database migrations.
- CLI foundation.
- REST API foundation.
- Health checks.
- systemd packaging.
- Unit test infrastructure.

Deliverable: a working Linux service with validated configuration and database connectivity.

## PHASE 2 — SSH gateway

Implement:

- SSH server.
- SSH client.
- SSH proxy.
- Host verification.
- Interactive terminal.
- PTY.
- Terminal resizing.
- Connection tracking.
- Session lifecycle.
- Default-deny channel restrictions.

Deliverable: an authenticated user can connect through the gateway to an authorized Linux destination.

## PHASE 3 — Identity and SSH CA

Implement:

- OIDC.
- User mapping.
- Vault integration.
- SSH certificate issuance.
- Certificate validation.
- Certificate expiration.
- Principal restrictions.
- CA public key distribution.
- Certificate audit events.

Deliverable: a user can obtain a short-lived certificate and use it to access an authorized destination.

## PHASE 4 — Authorization and JIT

Implement:

- RBAC.
- Access policies.
- Server groups.
- Environment restrictions.
- Temporary access.
- Approval workflow.
- Emergency access.
- Automatic expiration.

Deliverable: access is granted only according to centrally enforced policies.

## PHASE 5 — Recording and auditing

Implement:

- Terminal recording.
- Session replay.
- Audit events.
- Recording storage.
- Hash verification.
- Graylog integration.
- Prometheus metrics.
- OpenTelemetry instrumentation.

Deliverable: privileged sessions can be audited and their recordings retrieved by authorized auditors.

## PHASE 6 — Web console

Implement:

- Dashboard.
- User management.
- Server inventory.
- Policy management.
- Access requests.
- Approvals.
- Session management.
- Recording playback.
- Security settings.

Deliverable: a complete administrative interface.

## PHASE 7 — Enterprise readiness

Implement:

- HA.
- Multiple gateways.
- Production deployment.
- Backup and recovery.
- Upgrade procedures.
- Security hardening.
- Performance tests.
- Documentation.
- Release packaging.

Deliverable: a production-oriented release candidate.

---

# 23. DOCUMENTATION REQUIREMENTS

Documentation is part of the product, not an optional final step.

Write comprehensive documentation in English.

The README must explain:

- What the product is.
- Why it exists.
- Main features.
- Architecture.
- Security model.
- Requirements.
- Installation.
- Configuration.
- Vault integration.
- SSH CA setup.
- Linux server enrollment.
- First administrator setup.
- User management.
- Access workflows.
- CLI usage.
- Web console.
- API.
- Troubleshooting.
- Development.
- Testing.
- Contribution.

Also provide practical deployment guides.

Required guides:

1. Ubuntu Server installation.
2. Debian installation.
3. Rocky Linux installation.
4. AlmaLinux installation.
5. HashiCorp Vault integration.
6. Keycloak integration.
7. PostgreSQL deployment.
8. SSH CA configuration.
9. Destination Linux server configuration.
10. Graylog integration.
11. Backup and restore.
12. High availability.
13. Security hardening.
14. Troubleshooting.

Every documented command must correspond to an actual implemented feature.

Do not document nonexistent functionality.

---

# 24. ENGINEERING RULES FOR OPENCODE

These rules are mandatory.

1. Inspect the existing repository before changing anything.
2. Do not assume that files, modules, or infrastructure exist.
3. Do not delete existing files without explicit justification.
4. Do not overwrite existing configuration blindly.
5. Do not invent successful test results.
6. Do not implement fake Vault integrations.
7. Do not create mock authentication and describe it as production-ready.
8. Do not implement cryptographic algorithms from scratch.
9. Do not expose credentials in logs.
10. Do not use insecure defaults.
11. Do not introduce dependencies without evaluating their maintenance and security.
12. Do not generate enormous files containing unrelated responsibilities.
13. Keep modules small and maintainable.
14. Write tests alongside implementation.
15. Update documentation alongside implementation.
16. Use meaningful Git commits only when explicitly authorized.
17. Never push changes to a remote repository without explicit authorization.
18. Do not deploy to production infrastructure without explicit authorization.
19. Do not connect to real infrastructure or modify real servers during development unless explicitly authorized.
20. Report limitations honestly.

When a requirement is ambiguous:

- Inspect existing implementation.
- Identify the architectural impact.
- Choose the safest reasonable approach.
- Document the decision.
- Ask for clarification if the decision could compromise security or destroy existing work.

Do not stop after generating architecture documents.

Continue with implementation, testing, and documentation in controlled phases.

---

# 25. ACCEPTANCE CRITERIA

The initial product is not complete merely because it compiles.

The following must be demonstrated through tests:

### SSH functionality

- User can authenticate.
- Unauthorized users cannot authenticate.
- Authorized users can access permitted destinations.
- Unauthorized destinations are denied.
- PTY works.
- Terminal resizing works.
- Connections are tracked.
- Session termination works.

### Vault and certificates

- Vault integration works against a real test Vault instance.
- User public keys can be signed.
- Certificates have the configured validity.
- Invalid certificates are rejected.
- Expired certificates are rejected.
- Unauthorized principals are rejected.
- Revocation mechanisms are implemented and tested.
- CA private keys never leave the configured signing boundary.

### Authorization

- RBAC works.
- Default-deny is enforced.
- Environment policies work.
- JIT access expires.
- Approval requirements are enforced.
- Self-approval restrictions work where configured.

### Auditing

- Authentication events are recorded.
- Authorization decisions are recorded.
- Session metadata is recorded.
- Recording integrity can be verified.
- Sensitive credentials are not exposed.
- Recording access is permission-controlled.

### Operations

- Linux installation works.
- systemd service starts.
- Configuration validation works.
- Database migrations work.
- Health checks work.
- Backup and recovery are documented and tested.

### Quality

- Unit tests pass.
- Integration tests pass.
- Race tests pass.
- Static analysis passes.
- Security scanning is configured.
- Documentation reflects actual behavior.

Provide a final acceptance report with:

- Implemented features.
- Pending features.
- Test results.
- Security limitations.
- Known bugs.
- Deployment requirements.

---

# 26. FIRST EXECUTION INSTRUCTIONS

Start now.

Do not immediately generate the entire application.

First:

1. Inspect the current working directory.
2. Inspect the Git repository and current branch.
3. Inspect existing source files.
4. Inspect Go version and available development tools.
5. Identify existing project conventions.
6. Determine whether this is a new or existing project.
7. Produce the architecture and threat model.
8. Identify the minimum viable implementation.
9. Create the development roadmap.
10. Begin PHASE 1 after the architecture is established.

Work autonomously on implementation tasks that do not require external authorization.

Use the available OpenCode agents when appropriate, delegating independent tasks such as:

- Architecture and threat modeling.
- Go SSH gateway implementation.
- Vault integration.
- Database and API implementation.
- Frontend development.
- Testing and security review.
- Documentation.

However, maintain one coherent architecture and a single source of truth for API contracts, data models, and security policies.

Do not let independent agents introduce incompatible implementations.

Use explicit interfaces and integration tests between modules.

At the end of each phase:

1. Run tests.
2. Inspect the implementation.
3. Review security implications.
4. Update documentation.
5. Report completed work.
6. Identify remaining tasks.
7. Proceed to the next phase when safe.

The ultimate goal is a complete, independent, open-source enterprise SSH Bastion and Privileged Access Gateway written in Go, with HashiCorp Vault SSH CA integration, centralized identity, JIT access, RBAC, session recording, audit, and Linux-native deployment.

**Build a real security product, not a prototype disguised as production software.**
# nome: rscon
# RSCON
- Secure Infrastructure Access
Componente	Padrão
Produto	RSCON
CLI	rscon
Daemon	rscond
Configuração	/etc/rscon/config.yaml
Dados	/var/lib/rscon/
Logs	/var/log/rscon/
Container	ghcr.io/rsdenck/rscon
GitHub	github.com/rsdenck/rscon
Documentação	docs/


