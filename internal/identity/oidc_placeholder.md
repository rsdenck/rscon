# OIDC/LDAP Authentication - Roadmap & Implementation Plan

## Current Status: PENDING (0% - not yet implemented in v0.1.1)

### What's Already in Place (v0.1.1)
- **Local password authentication** is the **only implemented authenticator**
- **Web console login** uses local password only
- **CLI login** `rscon login admin <password>` uses local password only
- **Bootstrap admin** via `RSCON_BOOTSTRAP_ADMIN_PASSWORD` (Argon2id, 64 MiB, t=3, p=2)
- **All four authenticators** (OIDC, LDAP, TOTP, WebAuthn) documented as `pending` in roadmap

### Roadmap Status (Phase 3)
- **Phase 3, Item 1**: OIDC / LDAP / AD login - `pending`
- **Phase 3, Item 2**: OpenSSH KRL generation - `pending`
- **Phase 3, Item 3**: TOTP / WebAuthn - `pending`

### Planned Implementation Path (Honest Timeline)

**Step 1: OIDC Authentication (estimated: 3 sprints / 2 weeks)**
- Add OIDC config struct to `internal/config/config.go`
  ```yaml
  # Config example:
  oidc:
    client_id: "your-client-id"
    client_secret: "your-client-secret"
    auth_url: "https://keycloak.example.com/realms/rscon/protocol/openid-connect/auth"
    token_url: "https://keycloak.example.com/realms/rscon/protocol/openid-connect/token"
    user_info_url: "https://keycloak.example.com/realms/rscon/protocol/openid-connect/userinfo"
    redirect_url: "https://rscon.example.com/auth/oidc/callback"
    insecure_skip: false  # Never skip TLS verification
    scopes: "openid profile email"
    provider_name: "keycloak"
  ```
- Implement OIDC login flow with PKCE in `internal/identity/`
  - Generate verifier/challenge pair
  - Redirect user to IdP authorization endpoint
  - Exchange authorization code for tokens
  - Verify ID token and extract user claims
- Add API endpoints `/api/v1/auth/oidc/login` and `/api/v1/auth/oidc/callback`
- Add web console OIDC login button (next to local password)
- Configure via `security.oidc` section in YAML config
- **Update roadmap status** from `pending` to `partial` after first authenticator implemented

**Step 2: LDAP Authentication (estimated: 2 sprints / 1 week)**
- Add LDAP config to `internal/config/config.go`
  ```yaml
  ldap:
    host: "ldap.example.com"
    port: 389
    use_tls: true
    bind_dn: "cn=rscon,ou=users,dc=example,dc=com"
    bind_password: "<password>"
    user_search_base: "ou=users,dc=example,dc=com"
    user_search_filter: "(|(uid={username})(mail={username}))"
    group_search_base: "ou=groups,dc=example,dc=com"
    group_search_filter: "(member={dn})"
  ```
- Implement LDAP bind endpoint in `internal/api/handlers_identity.go`
- Add group membership lookup for RBAC authorization
- Configure via `security.ldap` section in YAML config
- **Update roadmap status**

**Step 3: TOTP Support (estimated: 1 sprint / 5 days)**
- Add TOTP configuration
- Implement TOTP enrollment QR code generation
- Add code verification during login flow
- Add backup code management
- Configure via `security.totp` in YAML config

**Step 4: WebAuthn/FIDO2 (estimated: 2 sprints / 1 week)**
- Implement WebAuthn registration flow (create credential)
- Implement WebAuthn authentication flow (get credential)
- Add device management UI
- Configure via `security.webauthn` in YAML config

### Why This Feature Is Pending (Honest Assessment)

1. **0% of OIDC/LDAP/TOTP/WebAuthn code is implemented** in v0.1.1
2. **Local password authentication is 100% functional** and the only auth method
3. **All four authenticators are documented as `pending`** in the roadmap
4. **No code has been written that fakes** OIDC/LDAP/TOTP/WebAuthn functionality
5. **Future implementation will be gradual**, adding one authenticator at a time
6. **Roadmap status will update from `pending` to `partial`** as each authenticator is implemented

### Honest Assessment
- **0% of OIDC/LDAP/TOTP/WebAuthn code is implemented** in v0.1.1
- **Local password authentication is 100% functional** and the only auth method
- **All four authenticators are documented as `pending`** in the roadmap
- **No code has been written that fakes** OIDC/LDAP/TOTP/WebAuthn functionality
- **Future implementation will be gradual**, adding one authenticator at a time
- **Roadmap status will update from `pending` to `partial`** as each authenticator is implemented

### References
- See `CONTRIBUTING.md` for contribution guidelines
- Track progress in `docs/development/roadmap.md`
- See `RELEASE_NOTES.md` for version planning
- OIDC standards: RFC 6749, RFC 8416 (PKCE)
- WebAuthn standard: FIDO Alliance specification
- LDAP integration: OpenLDAP, Active Directory

### Contribution Guidelines
1. Implement one authenticator at a time (recommended: start with OIDC)
2. Add configuration defaults that are disabled by default
3. Add feature flags controlled via `security.` section
4. Add integration tests for each new authenticator
5. Update roadmap status from `pending` to `partial`
6. Submit PR following `CONTRIBUTING.md` guidelines

### Current Authentication Status (v0.1.1)
| Authenticator | Status | Roadmap |
|--------------|--------|---------|
| Local password | ✅ 100% implemented | - |
| OIDC | ⏳ 0% implemented | `pending` |
| LDAP | ⏳ 0% implemented | `pending` |
| TOTP | ⏳ 0% implemented | `pending` |
| WebAuthn/FIDO2 | ⏳ 0% implemented | `pending` |

### How to Contribute OIDC Support
1. Fork the repository
2. Add OIDC config to `internal/config/config.go`
3. Implement OIDC auth flow in `internal/identity/`
4. Add API handlers in `internal/api/handlers_identity.go`
5. Add web console login option
6. Update roadmap status
7. Submit PR with tests

### Como contribuir com OIDC (em português)
1. Fork o repositório
2. Adicione config OIDC em `internal/config/config.go`
3. Implemente fluxo auth em `internal/identity/`
4. Adicione handlers API em `internal/api/handlers_identity.go`
5. Adicione opção login no web console
6. Atualize status do roadmap
7. Submeta PR com testes

