# OIDC/LDAP Authentication - Pending Implementation (Updated)

## Status: PENDING (not yet implemented)

### Current State
- **Local password authentication** is the **only implemented authenticator** in RSCON v0.1.1
- **OIDC (OpenID Connect)**, **LDAP**, **TOTP**, and **WebAuthn** are **not yet implemented**
- All four authenticators are listed as `pending` in `docs/development/roadmap.md`

### Planned Implementation (future release)

When OIDC/LDAP/TOTP/WebAuthn support is added, the following will be implemented:

1. **OIDC Authentication** - OpenID Connect flow with PKCE, support for popular IdPs
2. **LDAP/LDAPS Authentication** - Active Directory and OpenLDAP bind support
3. **TOTP Support** - TOTP enrollment QR code generation and code verification
4. **WebAuthn/FIDO2** - Registration and authentication flows

### References
- See `CONTRIBUTING.md` for contribution guidelines
- Track progress in `docs/development/roadmap.md`
- See `RELEASE_NOTES.md` for version planning
- OIDC standards: RFC 6749, RFC 8416 (PKCE)
- WebAuthn standard: FIDO Alliance specification
