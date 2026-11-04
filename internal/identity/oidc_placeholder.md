# OIDC/LDAP Authentication - Implementation Roadmap

## Current Status: PENDING (0% - documented as not yet implemented)

### What's Already in Place (v0.1.1)
- **Local password authentication** is the **only implemented authenticator**
- **OIDC (OpenID Connect)** support is documented as `pending` in the roadmap
- **LDAP/LDAPS** support is documented as `pending`
- **TOTP** support is documented as `pending`
- **WebAuthn/FIDO2** support is documented as `pending`

### Roadmap Status
- **Phase 3, Item 1**: OIDC / LDAP / AD login - `pending`
- **Phase 3, Item 2**: OpenSSH KRL generation and distribution - `pending`
- **Phase 3, Item 3**: TOTP / WebAuthn - `pending`

### Planned Implementation Path

When OIDC/LDAP/TOTP/WebAuthn support is added, the following sequence is planned:

**Step 1: OIDC Authentication (estimated: 2 sprints)**
- Add OIDC config struct to `internal/config/config.go`
- Implement OIDC login flow with PKCE in `internal/identity/`
- Add `/api/v1/auth/oidc/login` and `/api/v1/auth/oidc/callback` endpoints
- Add web console OIDC login button
- Configure via `security.oidc` section in YAML config
- Update roadmap status from `pending` to `partial` after first authenticator

**Step 2: LDAP Authentication (estimated: 2 sprints)**
- Add LDAP config to `internal/config/config.go`
- Implement LDAP bind endpoint in `internal/api/handlers_identity.go`
- Add group membership lookup for RBAC
- Configure via `security.ldap` section in YAML config
- Update roadmap status

**Step 3: TOTP Support (estimated: 1 sprint)**
- Add TOTP enrollment QR code generation
- Implement code verification during login
- Add backup code management
- Configure via `security.totp` in YAML config

**Step 4: WebAuthn/FIDO2 (estimated: 2 sprints)**
- Implement WebAuthn registration flow
- Implement WebAuthn authentication flow
- Add device management UI
- Configure via `security.webauthn` in YAML config

### References
- See `CONTRIBUTING.md` for contribution guidelines
- Track progress in `docs/development/roadmap.md`
- See `RELEASE_NOTES.md` for version planning
- OIDC standards: RFC 6749, RFC 8416 (PKCE)
- WebAuthn standard: FIDO Alliance specification
- LDAP integration: OpenLDAP, Active Directory

### Honest Assessment
- **0% of OIDC/LDAP/TOTP/WebAuthn code is implemented** in v0.1.1
- **Local password authentication is 100% functional** and the only auth method
- **All four authenticators are documented as `pending`** in the roadmap
- **No code has been written that fakes** OIDC/LDAP/TOTP/WebAuthn functionality
- **Future implementation will be gradual**, adding one authenticator at a time
- **Roadmap status will update from `pending` to `partial`** as each authenticator is implemented

### How to Contribute
1. Implement one authenticator at a time (recommended: start with OIDC)
2. Add configuration defaults that are disabled by default
3. Add feature flags controlled via `security.` section
4. Add integration tests for each new authenticator
5. Update roadmap status from `pending` to `partial`
6. Submit PR following `CONTRIBRIBUTING.md` guidelines
