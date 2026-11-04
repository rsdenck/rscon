# Security model

## Security invariants

1. No SSH connection reaches a destination without a fresh authorization
   decision.
2. The CA private key is not present in the gateway process when Vault is
   enabled. Production startup rejects the development file CA.
3. User private keys are never requested, transmitted or persisted.
4. Passwords are stored only as Argon2id hashes; API tokens only as SHA-256
   digests.
5. Forwarding is default-deny: agent and X11 are always rejected; TCP
   forwarding requires both configuration and policy permission.
6. Audit persistence failure fails closed for privileged operations when
   `audit.require_persist` is true.
7. TLS verification cannot be disabled through configuration.
8. There are no hardcoded credentials. Secrets come from environment variables
   or files with restrictive permissions.

## Authentication

### SSH users

- Primary method: OpenSSH user certificate signed by the RSCON CA.
  - Valid for `ssh.certificate_ttl` (default 10m).
  - Principal = RSCON username.
  - Critical extension `rscon-role=user`.
  - Extension `rscon-target=<server-id>:<account>` binds the certificate to one
    destination and Linux account.
  - Extension `rscon-mfa=true` is present only when the issuing session was MFA
    verified.
- Secondary method (opt-in, `ssh.allow_local_key_auth`): registered public
  keys, looked up by SHA-256 fingerprint. Disabled by default.
- Password authentication is not offered on the SSH port.

### API users

- Local bootstrap administrator authenticated with Argon2id; account lockout
  after N failures; login rate limited per IP and username.
- API token: 32 random bytes, returned once, stored as SHA-256, expires with
  `auth.session_ttl`, revocable.
- Browser sessions use an HttpOnly, SameSite=Strict cookie plus a CSRF header
  for unsafe methods. `Secure` is set when TLS is configured.
- OIDC is the intended enterprise authenticator; it is not implemented in
  v0.1. The schema (`identity_providers`) and identity mapping are prepared.

### MFA

MFA state is explicit on the principal. Policies can require MFA; the
authorization engine denies when the principal is not MFA-verified. The local
administrator cannot fake MFA. In v0.1 this means a policy requiring MFA is
effectively closed to local-only identities — intentional fail-closed
behavior. MFA enrollment is pending with OIDC.

## Authorization

Two layers:

1. **Capability (RBAC):** roles grant permissions such as `ssh.connect`,
   `access.approve`, `server.manage`, `audit.read`. Wildcards are supported.
2. **Access policy (ABAC):** explicit allow/deny rules scoped by subject
   (user/group/role), action, server, server group, environment, Linux
   account, channel and time window. Deny always wins; absence of an allow
   means deny.

Requirements (`require_approval`, `require_mfa`, maximum duration, channels)
are aggregated conservatively from all matching allow policies. Approval
grants are evaluated at issuance and on every new SSH connection.

Self-approval is rejected unless `security.allow_self_approval` is explicitly
enabled.

## Secrets and cryptography

- Argon2id (64 MiB, t=3, p=2) for passwords.
- SHA-256 for token digests, recording hashes and the audit chain.
- OpenSSH certificate formats and signatures only; no cryptographic primitive
  is implemented in this repository.
- TLS uses the system trust store or a configured CA bundle. `InsecureSkipVerify`
  is never set.
- UUIDv4 identifiers are generated with `crypto/rand`.

## Logging and redaction

Security events include timestamp, event ID, actor, action, result, resource,
source address and correlation ID. The following are never logged: passwords,
tokens, AppRole secret IDs, private keys, signed certificates' private
material, terminal contents in plaintext logs. Recordings are separate,
access-controlled objects.

## Operational security

- Service runs as a dedicated non-root user (`rscon`).
- SSH listens on an unprivileged port; a privileged-port strategy (firewall
  redirect or socket activation) is documented rather than running as root.
- Configuration is validated before startup and by `rscond config validate`.
- Database and recording directories are 0700; the gateway key is 0600.
- `rscond doctor` checks configuration, CA, Vault and database connectivity.
- Dependency scanning (`govulncheck`) and static analysis are part of CI.

## Known limitations

See `docs/development/roadmap.md` and the final acceptance report. The most
important: OIDC, S3 recordings, KRL generation and distributed session
termination are not implemented. Command filtering is explicitly not a
security boundary.
