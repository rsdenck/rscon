# RSCON Security Policy

## Supported Versions

| Version | Supported | Notes |
|---------|-----------|-------|
| v0.1.0 (current) | :white_check_mark: | Actively developed; security fixes released promptly |

## Reported Vulnerabilities

- Send reports to: maintainers@rscon.example (or open a private issue)
- Expected response time: 72 hours for acknowledgment, 2 weeks for critical fixes

## Security Practices Enforced in RSCON

### Default Deny
- `security.default_deny: true` in configuration; cannot be disabled
- All SSH channels (agent forwarding, X11 forwarding, TCP forwarding) disabled by default
- Policies must explicitly allow access; absent policy = denied

### Certificate Authority
- **Production**: HashiCorp Vault SSH secrets engine is the only supported CA
- **Development**: `dev.insecure_file_ca` explicitly opt-in; refused when `environment: production`
- **Never**: CA private key embedded in the gateway binary when Vault is enabled
- Certificate principals include both RSCON username and Linux account for dual-auth flow

### Key Management
- **No private keys stored**: Only public key fingerprints and certificate PEMs are persisted
- Gateway ephemeral key generated at startup if not persisted on disk
- User keys never leave the client or Vault

### TLS Verification
- TLS verification **never disabled** in production
- `allow_insecure_http` only for local Vault development (127.0.0.1)
- Certificate pinning via host key verification (known_hosts or trusted CA files)

### Auditing
- **Mandatory**: every SSH authentication and authorization decision logged to hash-chained audit table
- Audit write failure blocks the privileged operation (fail-closed)
- Hash chain: advisory lock `0x5253434f4e`, genesis hash 64 zeros, canonical payload `jsonb::text`
- Verification endpoint: `GET /api/v1/audit/verify`

### Channels
- **Agent forwarding**: denied by default (`allow_agent_forwarding: false`)
- **X11 forwarding**: denied by default (`allow_x11_forwarding: false`)
- **TCP forwarding (direct-tcpip)**: denied by default (`allow_tcp_forwarding: false`); opt-in via policy

### Dependency Management
- No Docker; native Linux binaries only
- Podman rootless noted as recommended but not enforced; Docker forbidden per requirements
- Dependency chain: `go.mod` -> `go.sum` vetted; `go mod tidy` run regularly

## Security Scanning

- `go vet ./...` runs on every contribution
- `go build ./...` must pass without errors
- `go test -count=1 ./...` all packages pass
- Configuration `rscond -check` validates before startup
- Unknown YAML fields rejected (`dec.KnownFields(true)`)

## Adding a New Feature with Security Implications

1. Update `docs/development/roadmap.md` status
2. Document the new attack surface in the threat model
3. Ensure default-deny posture is maintained
4. Run full test suite: `go test -count=1 ./...`
5. `gofmt -w && go vet ./...`
6. List changes in CHANGELOG

## Dependencies

- Review new dependencies with `go list -m all`
- Prefer `golang.org/x/` packages from the approved list
- No CGo unless absolutely necessary; approved with security review
