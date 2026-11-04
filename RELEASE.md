# RSCON v0.1.0 Release

## Binary Downloads
- `rscond` - The daemon (SSH + API server): 23.5MB
- `rscon` - The CLI client: 9.6MB

## Installation
### Native Linux Binary
1. Download the appropriate binary for your architecture
2. `chmod +x rscond rscon`
3. `sudo mv rscond /usr/local/bin/`
4. `sudo mv rscon /usr/local/bin/`
5. `sudo cp examples/config.yaml /etc/rscon/config.yaml`
6. Ensure PostgreSQL is running and configured
5. `sudo systemctl enable --now rscond`

### Docker/Podman (not recommended per requirements, but possible)
- Image: `ghcr.io/rsdenck/rscon` (to be packaged)

## Quick Start
```bash
# 1. Set bootstrap password
export RSCON_BOOTSTRAP_ADMIN_PASSWORD='StrongPassword123!'

# 2. Start rscond
rscond -config /etc/rscon/config.yaml

# 3. Login via CLI
rscon login admin <password>

# 4. Add a destination
rscon servers add my-server my-server my-server.com 22 production admin

# 5. Request JIT access
rscon access request my-server admin "Rotate credentials" 60

# 6. Approve the request
rscon access approve <request_id> approve

# 7. Connect via SSH
ssh -J deploy@my-server.com deploy my-server.com

# 8. Audit and recordings
rscon audit verify
rscon recording download <session_id> /path/to/recording.cast
```
