# End-to-End SSH Gateway Test

## Overview
This test verifies the complete RSCON data plane workflow from SSH connection to session termination with recording.

## Prerequisites
- rscond daemon running with configuration
- PostgreSQL database with migrations applied
- SSH client (OpenSSH)
- rscon CLI client

## Test Steps

### Step 1: Start rscond
```bash
rscond -config /etc/rscon/config.yaml &
# or in background:
nohup rscond -config /etc/rscon/config.yaml > /var/log/rscond.log 2>&1 &
```

### Step 2: Wait for Daemon Ready
```bash
# Wait for API to respond
curl -s http://127.0.0.1:18080/api/v1/health
# Should return: {"status":"healthy","database":"healthy","certificate_authority":"healthy","version":"0.1.0"}
```

### Step 3: Add a Destination Server
```bash
# Using rscon CLI
rscon servers add db-server db-server 192.168.1.50 22 production admin

# Or using API directly
curl -X POST http://127.0.0.1:18080/api/v1/servers \
  -H "Content-Type: application/json" \
  -d '{"name":"db-server","hostname":"db-server.example.com","address":"192.168.1.50","port":22,"environment_id":"production","default_account":"postgres"}'
```

### Step 4: Login
```bash
rscon login admin <password>
# Or via API
curl -X POST http://127.0.0.1:18080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"<password>"}'
```

### Step 5: Request JIT SSH Access
```bash
# Using rscon CLI
rscon access request db-server postgres "Rotate database credentials" 60

# Or via API
curl -X POST http://127.0.0.1:18080/api/v1/access-requests \
  -H "X-CSRF-Token: <csrf-token>" \
  -H "Cookie: rscon_session=<session-cookie>" \
  -H "Content-Type: application/json" \
  -d '{"server_id":"<server-id>","account":"postgres","reason":"Rotate credentials","requested_min":60}'
```

### Step 6: Approve the Request
```bash
# Using rscon CLI
rscon access approve <request_id> approve "Credentials rotation approved"

# Or via API
curl -X POST http://127.0.0.1:18080/api/v1/access-requests/<request_id>/decision \
  -H "X-CSRF-Token: <csrf-token>" \
  -H "Content-Type: application/json" \
  -d '{"decision":"approve","comment":"Credentials rotation approved"}'
```

### Step 7: Obtain SSH Certificate
```bash
# Using rscon CLI
rscon cert issue db-server postgres "rotation-key" 60

# Or via API (this generates a short-lived certificate)
curl -X POST http://127.0.0.1:18080/api/v1/certificates/issue \
  -H "X-CSRF-Token: <csrf-token>" \
  -H "Content-Type: application/json" \
  -d '{"server_id":"<server-id>","account":"postgres","ticket":"rotation-key","requested_min":60}'
```

### Step 8: SSH Connection
```bash
# The certificate grants automatic access
ssh deploy@192.168.1.50

# Or with the generated certificate
ssh -i <(printf '-----BEGIN RSA PRIVATE KEY-----\n<cert-pem>\nEND RSA PRIVATE KEY-----\n') deploy@192.168.1.50
```

### Step 9: Verify Session
```bash
# Check active sessions
rscon sessions list

# Or via API
curl http://127.0.0.1:18080/api/v1/sessions
```

### Step 10: Terminate Session
```bash
# Using rscon CLI
rscon sessions list  # Note session ID

# Or via API
# Terminate via API or kill the process

# Or wait for idle timeout (default 30min) or max session duration (default 8h)
```

### Step 11: Check Recording
```bash
# If recording was enabled, the session will have a recording
rscon recording download <session_id> /path/to/recording.cast

# Or via API
curl -O http://127.0.0.1:18080/api/v1/sessions/<session_id>/recording -o recording.cast
```

### Step 12: Audit Verification
```bash
# Verify the hash chain is intact
rscon audit verify

# Or via API
curl http://127.0.0.1:18080/api/v1/audit/verify
```

## Expected Outcomes
- ✅ Destination registered and visible in UI
- ✅ JIT access request created and visible
- ✅ Approval grants immediate SSH access
- ✅ SSH connection succeeds with certificate auth
- ✅ Session recorded to asciicast v2 format
- ✅ Session terminates (idle timeout or manual)
- ✅ Recording saved and downloadable
- ✅ Audit hash chain verifies successfully
- ✅ All events logged to PostgreSQL

## Test Termination Scenarios
1. **Idle timeout**: Session automatically terminates after `idle_timeout` (default 30min)
2. **Max duration**: Session automatically terminates after `max_session_duration` (default 8h)
3. **Manual termination**: `rscon sessions list` → note ID → terminate
4. **Network loss**: Session handles graceful disconnect
5. **Certificate expiry**: Certificate reaches `MaxTime` and connection is rejected

## Test Data
| Component | Value |
|-----------|-------|
| Default SSH port | 22 |
| Default recording backend | filesystem |
| Default recording format | asciicast v2 |
| Default certificate TTL | 10 minutes |
| Default destination cert TTL | 15 minutes |
| Default max session duration | 8 hours |
| Default idle timeout | 30 minutes |
| Default max concurrent sessions | 5 |

## Known Limitations (v0.1.0)
- S3 recording backend not available (filesystem only)
- Browser terminal not included
- OIDC/LDAP authentication not implemented
- Multi-tenant isolation not tested
- Firewall/NAT traversal not tested
