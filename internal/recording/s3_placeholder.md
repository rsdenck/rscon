# S3 Recording Backend - Pending Implementation

## Status: PENDING (not yet implemented)

This feature is documented as pending in the roadmap and will be implemented in a future release.

### Current State
- **Filesystem backend** is the **only implemented** recording backend in RSCON v0.1
- **S3/compatible backends** are **not yet implemented**
- All S3 recording support is listed as `pending` in `docs/development/roadmap.md`
- No code has been written for S3 recording support

### Current Recording Backends (implemented)
- **Filesystem**: Asciicast v2 format, SHA-256 integrity, fail-closed default
  - Files stored at: `/var/lib/rscon/recordings/`
  - File permissions: 0600
  - Directory permissions: 0700
  - Retention: configurable via `recording.retention`
  - Max file size: configurable via `recording.max_file_bytes` (default: 256MiB)

### Planned Implementation (future release)
When S3 recording backend support is added, the following will be implemented:

1. **S3 Backend Structure** (`internal/recording/s3_backend.go`):
   - Implementation of `recording.Store` interface
   - Support for Amazon S3 and MinIO S3-compatible storage
   - Configurable bucket name, region, endpoint URL
   - SSE-KMS and SSE-S3 encryption support
   - Presigned URL generation for secure download
   - Lifecycle rule support

2. **Configuration Updates** (`internal/config/config.go`):
   - Add `s3` section to YAML config:
     ```yaml
     s3:
       bucket_name: my-recordings
       region: us-east-1
       endpoint: s3.amazonaws.com  # or MinIO endpoint
       access_key_id: ""
       secret_access_key: ""
       use_path_style_endpoint: false
       sse_kms_key_id: ""
       sse_s3_algorithm: ""
     ```
   - Support environment variable overrides

3. **API Updates** (`internal/api/handlers_recording.go`):
   - Add backend selection when creating recordings
   - Add backend status endpoint
   - Support backend migration (filesystem -> S3)
   - Add bucket management UI endpoints

4. **Web Console Updates** (`web/src/`):
   - Add backend selection dropdown in recording settings
   - Add bucket management and testing UI
   - Add presigned URL display and download controls

### Migration Path
When S3 support is added:
1. Add S3 backend implementation without removing filesystem backend
2. Keep `backend: filesystem` as default
3. Add `backend: s3` option with required credentials
4. Add UI toggle to switch backends
5. Add roadmap status change from `pending` to `partial`
6. Add integration tests for S3 upload/download/presign

### References
- See `CONTRIBUTING.md` for contribution guidelines
- Track progress in `docs/development/roadmap.md`
- See `RELEASE_NOTES.md` for version planning
- AWS SDK for Go v2: https://github.com/aws/aws-sdk-go-v2
- MinIO Go SDK: https://github.com/minio/minio-go
- RFC 6365 (AWS SDK coverage)

### Roadmap Status
- **Current phase**: Pending (0% complete)
- **Estimated effort**: 2-3 sprints
- **Dependencies**: Configuration updates, API updates, web console updates
- **Will update to `partial` status** when S3 backend is implemented

