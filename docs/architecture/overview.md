# RSCON — Architecture Overview

RSCON (Secure Infrastructure Access) is an SSH bastion and privileged access
gateway written in Go. It centralizes SSH access to Linux fleets, issues
short-lived OpenSSH certificates, enforces centralized authorization, records
interactive sessions and produces tamper-evident audit trails.

The product is an independent implementation. It is not a wrapper around
OpenSSH, The Bastion, Teleport or JumpServer.

## Design goals

- **Default deny.** No destination, Linux account or SSH channel is reachable
  unless an explicit policy allows it.
- **Short-lived credentials.** Users authenticate with SSH certificates
  (default 10 minutes). The certificate authority private key never lives in
  the gateway process when Vault is enabled.
- **One control plane, one data plane.** The control plane (API, database,
  policy, approvals, audit) is logically independent from the data plane (SSH
  proxy). Both run in a single binary today; the boundary is enforced by Go
  interfaces so they can be split later.
- **Auditable by construction.** Security-sensitive operations write to a
  hash-chained audit table. A failed mandatory audit write fails the operation.
- **Linux-native.** Static binary, systemd unit, dedicated service account,
  no container runtime required.

## Logical components

```text
                    ┌─────────────────────────────────────────────┐
   Browser / CLI    │                  CONTROL PLANE              │
        │           │  REST API  ·  RBAC  ·  JIT approvals        │
        │  HTTPS    │  inventory · certificates · audit · metrics │
        ▼           └───────────────┬─────────────────────────────┘
 ┌──────────────┐                   │ SQL (pgx)
 │  rscon CLI   │                   ▼
 └──────┬───────┘            ┌────────────┐        ┌──────────────┐
        │ SSH + cert         │ PostgreSQL │        │ HashiCorp    │
        ▼                    └────────────┘        │ Vault        │
 ┌──────────────────────────────┐                 │ SSH CA       │
 │         SSH GATEWAY          │──── sign ──────▶│ (private key │
 │ auth · policy · proxy · rec  │                 │  stays here) │
 └──────────────┬───────────────┘                 └──────────────┘
                │ SSH (gateway certificate)
                ▼
        ┌────────────────┐
        │ Linux servers  │  trust the RSCON user CA
        │ (sshd)         │  through TrustedUserCAKeys
        └────────────────┘
```

## Two access modes

1. **Proxy mode (default).** The user connects to the gateway with a
   short-lived certificate whose principal is their RSCON identity and whose
   critical extension binds it to one destination and Linux account. The
   gateway re-evaluates authorization, obtains a separate short-lived
   certificate for its own data-plane key (principal = destination account),
   verifies the destination host key, and proxies the session. The user's
   private key never leaves their machine.
2. **Direct certificate mode.** The user requests a certificate from the API
   and uses it directly against a destination that trusts the same CA. Useful
   for SCP/SFTP tooling and for environments where the gateway is not in the
   data path.

## Trust boundaries

- The **CA private key** exists only inside Vault. In development, an explicit
  opt-in file-based CA may be used; production startup refuses it.
- The **gateway private key** lives on the gateway filesystem (0600) and is
  never stored in PostgreSQL or logs.
- **PostgreSQL** stores identities, inventory, policy, metadata and public
  certificates. It never stores user private keys.
- The **browser** never talks to Vault. All privileged operations happen in
  the API process.
- **Destinations** trust the RSCON certificate authority; they do not need
  network access to RSCON.

## Non-goals for v0.1

- OIDC/LDAP/AD login providers (schema and interfaces are prepared, but the
  local bootstrap administrator is the only implemented authenticator).
- S3 recording backend (storage is behind an interface; filesystem is
  implemented).
- Command-level filtering as a security boundary. RSCON records and confines
  sessions; strong command restriction must be enforced on the destination
  with restricted accounts or forced commands.

These limitations are tracked in `docs/development/roadmap.md` and the
acceptance report.
