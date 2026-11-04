# Components and module boundaries

| Package | Responsibility |
|---|---|
| `cmd/rscond` | Daemon entry point, subcommands (`serve`, `migrate`, `doctor`, `keys`, `version`). |
| `cmd/rscon` | Operator CLI. |
| `internal/config` | Strict YAML loading, defaults, validation. |
| `internal/identity` | UUIDs, opaque tokens, Argon2id password hashing. |
| `internal/model` | Domain types shared by store, policy and API. |
| `internal/database` | pgx pool, embedded migrations, health. |
| `internal/store` | PostgreSQL repositories; the only SQL in the product. |
| `internal/authz` | RBAC + access-policy evaluation, grants, decisions. |
| `internal/vault` | Vault HTTP client: AppRole, token renewal, SSH signing, health. |
| `internal/ca` | Signer interface and implementations (Vault, dev file CA). |
| `internal/hostkey` | Destination host-key verification (known_hosts, pins). |
| `internal/recording` | Asciicast v2 writer, SHA-256 integrity, filesystem storage. |
| `internal/audit` | Hash-chained audit events, slog, syslog/GELF fan-out. |
| `internal/observability` | Prometheus metrics and request instrumentation. |
| `internal/gateway` | SSH server, authentication, authorization, proxy, session lifecycle. |
| `internal/api` | Versioned REST API, middleware, DTOs. |
| `internal/web` | Embedded admin UI build (served by the API). |
| `pkg/client` | Typed API client used by the CLI. |
| `pkg/version` | Build metadata. |

## Dependency direction

```text
cmd ──▶ api ──▶ authz ──▶ model
 │        │        │
 │        ├──▶ store ──▶ database ──▶ model
 │        │        │
 │        ├──▶ ca ──▶ vault
 │        └──▶ recording / audit / observability
 └──▶ gateway ──▶ authz · store · ca · hostkey · recording · audit
```

`store` and `database` never import `api` or `gateway`. `gateway` consumes
narrow interfaces for the data it needs, which keeps the SSH data plane
independently testable and allows extracting it into a separate service.

## Data plane interfaces

```go
type Store interface {
    GetUserByID(ctx, id) (*model.User, error)
    GetUserByUsername(ctx, name) (*model.User, error)
    GetUserByKeyFingerprint(ctx, fp) (*model.User, error)
    GetServerByName(ctx, name) (*model.Server, error)
    ListServerGroupIDs(ctx, serverID) ([]string, error)
    CreateSession(...)
    CloseSession(...)
    ...
}

type Authorizer interface {
    Authorize(ctx, principal, action, resource, opts) (authz.Decision, error)
}

type Signer interface {
    CA(ctx) (ssh.PublicKey, error)
    Sign(ctx, ca.SignRequest) (*ca.SignedCertificate, error)
}
```

## Control plane interfaces

The API depends on the store and the policy engine only. Certificate issuance
goes through `ca.Signer`; the API never sees CA private key material. Vault
credentials are read from files/environment by the Vault client, never
returned by an endpoint.

## Deployment topology

Single node (v0.1): one `rscond` process serving SSH (2222/tcp) and HTTPS API
(8080/tcp), one PostgreSQL, one Vault.

Scale-out: N `rscond` nodes behind an L4 load balancer for SSH and an L7 load
balancer for HTTPS. They share PostgreSQL and Vault. Session metadata carries
the gateway name; on startup each gateway reconciles sessions it owned that
were left active. Active TCP connections do not survive a node failure and
cannot be resumed; this is documented in `docs/operations/high-availability.md`.
