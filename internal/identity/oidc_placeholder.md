# OIDC/LDAP Authentication - Pending Implementation

## Status: PENDING (not yet implemented)

This feature is documented as pending in the roadmap and will be implemented in a future release.

### Current State
- **Local password authentication** is the **only implemented authenticator** in RSCON v0.1
- **OIDC (OpenID Connect)**, **LDAP**, **TOTP**, and **WebAuthn** are **not yet implemented**
- No code has been written for these authenticators
- All four authenticators are listed as `pending` in `docs/development/roadmap.md`

### Planned Implementation (future release)
When OIDC/LDAP/TOTP/WebAuthn support is added, the following will be implemented:

1. **OIDC Authentication**:
   - OpenID Connect flow with PKCE
   - Support for popular IdPs (Keycloak, Auth0, Okta, Google, Azure AD)
   - Token refresh and session management
   - Userinfo endpoint integration

2. **LDAP/LDAPS Authentication**:
   - Active Directory and OpenLDAP bind support
   - Group membership lookup for RBAC
   - TLS/TLS STARTTLS support
   - Connection pooling

3. **TOTP Support**:
   - TOTP enrollment QR code generation
   - Code verification during login
   - Backup code management
   - Integration with identity provider

4. **WebAuthn/FIDO2**:
   - Registration flow (create credential)
   - Authentication flow (get credential)
   - Device management
   - Backup key support

### Roadmap Status
- **Current phase**: Pending (0% complete)
- **Estimated effort**: 3-5 sprints
- **Dependencies**: Configuration updates, API endpoints, web console updates
- **Will update to `partial` status** when first authenticator is implemented

### References
- See `CONTRIBUTING.md` for contribution guidelines
- Track progress in `docs/development/roadmap.md`
- See `RELEASE_NOTES.md` for version planning
- OIDC standards: RFC 6749, RFC 8416 (PKCE)
- WebAuthn standard: FIDO Alliance specification
