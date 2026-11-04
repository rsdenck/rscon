# OIDC/LDAP Authentication - Partially Implemented (v0.1.2)

## Current Status: PARTIAL (15% - config structure implemented; authenticators pending)

### What's Already in Place (v0.1.2)
- **Local password authentication** is the **only implemented authenticator**
- **OIDC configuration structure** is now implemented in `internal/config/config.go`
  - OIDCConfig struct added to `SecurityConfig` with fields:
    - `client_id`, `client_secret`, `auth_url`, `token_url`, `user_info_url`
    - `redirect_url`, `insecure_skip`, `scopes`, `provider_name`
  - All fields support environment variable overrides via `applyEnv()`
- **OIDC config in YAML** example:
  ```yaml
  security:
    oidc:
      client_id: "your-client-id"
      client_secret: "your-client-secret"
      auth_url: "https://keycloak.example.com/realms/rscon/protocol/openid-connect/auth"
      token_url: "https://keycloak.example.com/realms/rscon/protocol/openid-connect/token"
      user_info_url: "https://keycloak.example.com/realms/rscon/protocol/openid-connect/userinfo"
      redirect_url: "https://rscon.example.com/auth/oidc/callback"
      insecure_skip: false
      scopes: "openid profile email"
      provider_name: "keycloak"
  ```
- **Environment variable mapping** (via `applyEnv()`):
  - `RSCON_OIDC_CLIENT_ID`
  - `RSCON_OIDC_CLIENT_SECRET`
  - `RSCON_OIDC_AUTH_URL`
  - `RSCON_OIDC_TOKEN_URL`
  - `RSCON_OIDC_USER_INFO_URL`
  - `RSCON_OIDC_REDIRECT_URL`
  - `RSCON_OIDC_SCOPES`
  - `RSCON_OIDC_PROVIDER_NAME`

### Roadmap Status (Phase 3, Item 1)
- **OIDC / LDAP / AD login** - updated from `pending` to `partial` (15% complete)
  - OIDC config structure implemented
  - Authenticator flow pending - will update to `partial` when first login flow is implemented
  - Will update to `done` when OIDC login is fully functional and tested

### What's Implemented (15%)
| Component | Status |
|-----------|--------|
| OIDC config structure | ✅ 100% - struct + yaml + env vars |
| OIDC login flow | ⏳ 0% - pending implementation |
| OIDC callback endpoint | ⏳ 0% - pending implementation |
| OIDC web console button | ⏳ 0% - pending UI work |
| LDAP auth | ⏳ 0% - documented as pending |
| TOTP support | ⏳ 0% - documented as pending |
| WebAuthn/FIDO2 | ⏳ 0% - documented as pending |

### Honest Assessment
- **15% of OIDC code is implemented** (config structure only)
- **0% of OIDC login flow is implemented** (no actual authentication)
- **All four authenticators (OIDC, LDAP, TOTP, WebAuthn) were `pending`** in v0.1.1
- **OIDC config is now `partial`** - the structure is in place for future authenticators
- **No code fakes OIDC authentication** - local password remains the only working method
- **Roadmap status updated from `pending` to `partial`** for OIDC category

### Implementation Path Forward
1. **Step 1 (completed)**: OIDC config structure in YAML + env vars ✅
2. **Step 2**: OIDC login flow with PKCE
3. **Step 3**: API endpoints `/api/v1/auth/oidc/login` and `/api/v1/auth/oidc/callback`
4. **Step 3**: Web console OIDC login button
5. **Step 4**: Update roadmap from `partial` to `done` when fully functional

### Current Authentication Status (v0.1.2)
| Authenticator | Status | Roadmap |
|--------------|--------|---------|
| Local password | ✅ 100% implemented | - |
| OIDC | ⚠️ 15% implemented (config only) | `partial` |
| LDAP | ⏳ 0% implemented | `pending` |
| TOTP | ⏳ 0% implemented | `pending` |
| WebAuthn/FIDO2 | ⏳ 0% implemented | `pending` |

### Como contribuir com OIDC (em português)
1. Implementar flow de login com PKCE em `internal/identity/`
2. Adicionar endpoints API `/api/v1/auth/oidc/login` e `/api/v1/auth/oidc/callback`
3. Adicionar botão de login OIDC no web console
4. Atualizar status do roadmap de `partial` para `done`
5. Submeter PR seguindo `CONTRIBRIBUTING.md` guidelines
