# Threat model

Method: STRIDE over the trust boundaries in `overview.md`. Risk is
`likelihood × impact` with a 5×5 scale before mitigations.

## Assets

1. SSH CA private key (Vault).
2. Gateway data-plane private key (filesystem).
3. User identities, credentials, API tokens.
4. Destination infrastructure and Linux accounts.
5. Session recordings and audit trail.
6. PostgreSQL contents (inventory, policy, metadata).
7. Vault AppRole credentials.

## Adversaries

- External unauthenticated attacker with network access to SSH/API ports.
- Authenticated low-privilege user attempting lateral movement or privilege
  escalation.
- Compromised user endpoint (stolen certificate or token).
- Malicious or careless insider with administrator rights.
- Compromised destination host.
- Attacker with read access to backups or recording storage.

## STRIDE summary

| Threat | Scenario | Mitigation | Residual risk |
|---|---|---|---|
| Spoofing | Attacker presents a self-signed user certificate | Certificates verified against configured CA public keys; principal must match an active RSCON user; certificate must exist in DB and be bound to the requested target | Stolen unexpired certificate (TTL 10m, serial revocation, session re-authorization) |
| Spoofing | Rogue destination host key | known_hosts, host CA or explicit fingerprint pin; insecure acceptance is impossible in config | Operator enrolls wrong fingerprint |
| Tampering | Modify audit rows | Hash chain + verification endpoint; DB privileges restricted; optional external syslog/GELF copy | DB superuser can rewrite the chain; ship logs off-box |
| Tampering | Modify recordings | SHA-256 recorded at close; verify on demand; filesystem 0600 and separate directory | No object-lock backend in v0.1 |
| Repudiation | User denies a session | Session row binds user, cert serial, source IP, target, timestamps; recording hash | NAT obscures source IP |
| Information disclosure | CA private key exfiltration | Key only in Vault; gateway never requests it; AppRole least privilege; TLS verified | Vault compromise |
| Information disclosure | Private keys/tokens in DB or logs | Private keys never stored; tokens stored as SHA-256; redaction in logs; no secrets in audit details | Terminal recording may contain secrets typed by users |
| Information disclosure | Unauthorized recording playback | `recording.read` permission, per-object authorization, audit of playback | Auditor account compromise |
| Denial of service | Auth brute force / resource exhaustion | Rate limits, lockout, MaxAuthTries=3, handshake timeout, connection caps, body limits | L3/L4 flood requires network controls |
| Elevation of privilege | Policy bypass through username parsing | Username parsed strictly (`account@server`); server/account re-authorized server-side; target binding extension checked | Destination misconfiguration (e.g. sudo) |
| Elevation of privilege | Agent/X11/TCP forwarding abuse | Denied by default; TCP forwarding requires both config and policy channel allow; agent/X11 always rejected | Operator enables forwarding explicitly |
| Elevation of privilege | Command filtering bypass | RSCON does not claim command security; docs require forced commands/restricted accounts on destination | Destination hardening is operator responsibility |
| SSRF | API fetches attacker URL | No user-supplied URL fetching; Vault address from config only | — |
| Supply chain | Malicious dependency | Minimal dependency set, `go.sum`, govulncheck/CI, pinned versions | Upstream compromise |

## Key attack paths and controls

### A. User → gateway SSH

1. TCP accepted; handshake deadline 30s; MaxAuthTries 3.
2. Public key callback accepts only OpenSSH user certificates whose signature
   chains to a trusted CA, whose `rscon-role` extension is `user`, whose
   principal maps to an active user, whose serial is not revoked, and whose
   `rscon-target` matches the requested `account@server`.
3. Authorization runs again with fresh policy and grants.
4. Failed attempts are audited (username, source IP, reason) without secrets.

### B. Gateway → destination

1. Destination resolved only from inventory (no arbitrary host syntax).
2. Host key verified before authentication.
3. Gateway certificate is short-lived, principal-bound to the account.
4. Proxy exposes only `session` and, when explicitly allowed, `direct-tcpip`.
5. Idle and maximum durations enforced by timers independent of the client.

### C. API

1. Bearer token or HttpOnly cookie; token hash lookup with expiry/revocation.
2. Cookie-authenticated unsafe requests require a CSRF header.
3. Authorization checked server-side for every route; frontend is not trusted.
4. Strict JSON decoding, 1 MiB body limit, unknown-field rejection.
5. Login rate limiting and per-account lockout.
6. CORS allowlist; secure cookie flags when TLS is enabled.

### D. Vault

1. AppRole credentials from files with restrictive permissions; no root token.
2. TLS verification always on, optional custom CA and SNI.
3. Token renewal; on hard failure certificate issuance fails closed.
4. Only public keys are sent for signing.

## Out of scope (v0.1)

- OIDC/LDAP federation, WebAuthn, TOTP.
- S3/Object-Lock recording storage.
- Distributed session termination across gateways.
- Kernel-level command sandboxing.
- Protection against a compromised gateway host (it holds data-plane keys and
  can observe plaintext terminal traffic by design).
