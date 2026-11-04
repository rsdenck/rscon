# Contributing to RSCON

## Development Workflow

1. **Branch from main**: `git checkout -b feature/X`
2. **Make a change**: implement the feature, fix the bug, add the test
3. **Run the full test suite**: `go test -count=1 ./...`
4. **Run `go vet`**: `go vet ./...`
5. **Run `gofmt`**: `gofmt -w ./...`
6. **Commit with a clear message**: see [Conventional Commits](https://conventionalcommits.org/)
7. **Push and open a PR**

## Code Style

- Go: `gofmt -w` on every save; `go vet` must pass with no warnings
- YAML: strict field validation; unknown fields rejected at parse time
- Imports: grouped standard library, third party, local; alphabetical within groups
- Error handling: explicit; use `fmt.Errorf` with `%w` for wrapping; sentinel errors in `internal/store`

## Adding a New Package

1. Create the directory under `internal/`
2. Create `go.mod`-aware package with package comment
3. Add to `go.mod` if new external dependency (run `go get`)
4. Add integration tests in `internal/store/` if PostgreSQL-backed
5. Update `docs/development/roadmap.md` if phase is complete

## Release Process

1. Update version in `pkg/version/version.go` (or via `-ldflags`)
2. Run `make build` and `make test`
3. Tag the release: `git tag vX.Y.Z`
4. Generate CHANGELOG from commit messages
5. Publish binaries and Docker/Podman images (if applicable)

## Testing Guidelines

- **Unit tests**: pure Go, no external dependencies; `go test -count=1 ./...`
- **Integration tests**: use real PostgreSQL; set `RSCON_TEST_DATABASE_URL`
- **Never commit test credentials** - use environment variables
- **Honest reporting**: if a feature is not yet implemented, document it as `pending` in the roadmap; do not claim it works

## Adding a New authenticator (OIDC, LDAP, TOTP, WebAuthn)

1. Implement the authenticator in `internal/identity/`
2. Add configuration to `internal/config/config.go`
3. Register the new authenticator type in the bootstrap logic
4. Add API endpoints in `internal/api/handlers_identity.go`
5. Add UI pages in `web/src/`
6. Update `docs/development/roadmap.md` to change status from `pending` to `partial`
7. Run `go test -count=1 ./...` to verify no regressions

## Security Disclosures

- Report security issues via private email to the maintainers
- Do not disclose vulnerabilities in public issues until they are resolved
- The threat model is documented in `docs/threat_model.md` (create if adding authn/ZT features)
