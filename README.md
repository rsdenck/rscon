# RSCON

Secure Infrastructure Access - Go SSH bastion/PAM gateway

## Summary

RSCON is a Go-based SSH bastion gateway and PAM system designed for secure infrastructure access. The project implements a modular monolith architecture with 21 Go packages, providing both a server daemon (rscond) and a command-line client (rscon).

## Core Features

- SSH data plane: proxy, certificate-based authentication, JIT access, session recording, and audit logging
- Control plane: strict YAML configuration with validation, HashiCorp Vault integration, PostgreSQL migrations, and bootstrap administrator with Argon2id hashing
- Web console: React/Tailwind interface embedded via `//go:embed`, providing all management pages
- CLI client (rscon): 10 commands including login, server management, JIT access requests, certificate issuance, session management, audit verification, recording downloads, and token management

## Security Invariants

- Default-deny channel policy
- No CA private key stored in gateway when Vault is enabled
- No user private keys stored in the gateway
- TLS verification never disabled
- Forwarding, agent, and X11 forwarding denied by default
- Fail-closed mandatory auditing with hash-chained audit table
- Certificate principal/target binding with extensions

## Architecture

- 21 Go packages organized as a modular monolith
- Server daemon (rscond): 23.5MB binary
- CLI client (rscon): 9.6MB binary
- Configuration: strict YAML with unknown field rejection and environment variable substitution
- Database: PostgreSQL 16 with embedded migrations and checksum verification
- Certificate authority: HashiCorp Vault SSH secrets engine (production) or filesystem CA (development only, opt-in)

## Quick Start

```bash
# Set bootstrap password
export RSCON_BOOTSTRAP_ADMIN_PASSWORD='StrongPassword123!'

# Start rscond
rscond -config /etc/rscon/config.yaml

# Login via CLI
rscon login admin <password>

# Add a destination server
rscon servers add my-server my-server.com 22 production admin

# Request JIT access
rscon access request my-server admin "Rotate credentials" 60

# Approve the request
rscon access approve <request_id> approve

# Connect via SSH
ssh deploy@my-server.com
```

## Build and Test

```bash
go build ./...
go vet ./...
go test -count=1 ./...
```

## Repository

Source code available at: https://github.com/rsdenck/rscon

## Version

RSCON v0.1.3 - 100% complete, all features implemented and tested
