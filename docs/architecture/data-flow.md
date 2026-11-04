# Data flows

## 1. Administrator bootstraps the platform

```text
operator ── rscond migrate ──▶ PostgreSQL schema + seed roles/environments
operator ── RSCON_BOOTSTRAP_ADMIN_PASSWORD=... rscond serve
             └─ first start: create admin user, Argon2id hash, bind super_admin
operator ── rscon login ──▶ POST /api/v1/auth/login
             └─ opaque token hash stored in api_tokens; token returned once
```

The bootstrap variable is consumed only when the `users` table is empty. It is
never written to disk. A local administrator exists for recovery and initial
setup; the design target is OIDC, which is not implemented in v0.1.

## 2. Server enrollment

```text
admin ── POST /api/v1/environments
admin ── POST /api/v1/server-groups
admin ── POST /api/v1/servers            (address, port, environment, fingerprint)
admin ── POST /api/v1/access-policies    (subjects, servers/groups, accounts, effect)
admin ── POST /api/v1/users/{id}/public-keys   (optional local-key identity)
```

The destination `sshd` must trust the RSCON user CA:

```text
TrustedUserCAKeys /etc/ssh/rscon_user_ca.pub
```

Host identity is verified against `known_hosts` and/or a host CA. Registration
never learns host keys by trusting an unverified handshake; the fingerprint
must be enrolled explicitly or the host key must chain to a configured host CA.

## 3. Certificate issuance (API)

```text
user ── POST /api/v1/certificates {server, account, public_key}
        │
        ├─ token authentication (hash lookup, expiry, revocation)
        ├─ authz.Authorize("ssh.connect", resource)
        │    ├─ permission check (roles -> permissions)
        │    ├─ matching allow policy, any deny wins, time window, MFA
        │    └─ approval grant lookup when policy requires it
        ├─ ca.Signer.Sign(public key, principal=[username], ttl, key_id,
        │                 extensions: rscon-role=user, rscon-target=<server>:<account>)
        ├─ store.CreateCertificate(serial, cert PEM, validity)
        └─ audit.Write(certificate.issued)  ── fail closed if required
```

Only the public key is sent to Vault. The user's private key stays on the
user's machine.

## 4. Proxy session

```text
user ── SSH handshake ──▶ gateway
  cert signed by trusted CA? ── no ──▶ reject
  validity / principal / extension rscon-role=user? ── no ──▶ reject
  serial revoked or certificate record missing? ── yes ──▶ reject
  username = account@server ; server enabled? ── no ──▶ reject
  authz.Authorize("ssh.connect") ── no ──▶ reject + audit
  gateway certificate from CA (principal=account, role=gateway)
  destination host key verified (known_hosts / CA / pin)
  session registered in DB
  recording started (fail closed when required)
  channels: session (shell/exec, pty, resize)
            direct-tcpip only if policy + config allow
            agent/x11/forwarded-tcpip denied
  every input/output event timestamped into asciicast v2
  close: hash recording, persist metadata, close session row, audit
```

## 5. Just-in-time access

```text
user ── POST /api/v1/access-requests {server, account, reason, duration}
        policy requires approval -> status=pending, audit
approver ── POST /api/v1/access-requests/{id}/approve
            permission access.approve, not the requester
            when required approval count reached: status=approved,
            starts_at=now, ends_at=now+duration
user ── certificate issuance succeeds only while an active grant exists
expiry ── the authorization engine ignores expired grants automatically
```

Grants are evaluated at issuance **and** at every SSH connection, so revoking
a grant or disabling a user takes effect on the next connection.

## 6. Audit and recording

```text
security event ──▶ audit.Writer
                   ├─ PostgreSQL audit_events (hash chain: hash = H(prev||event))
                   ├─ structured JSON log (slog)
                   └─ optional syslog / GELF (Graylog)
terminal bytes ──▶ asciicast file (0600) ──▶ SHA-256 ──▶ session_recordings
auditor ── GET /api/v1/recordings/{id}/download ── permission checked ──▶ stream
         ── GET /api/v1/audit/verify ── recompute chain and report first break
```
