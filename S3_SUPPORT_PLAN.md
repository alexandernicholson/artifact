# S3 Direct Storage Support - Implementation Plan

## Current Architecture

The artifact CLI currently:
1. Connects to a **Hub** service (Semaphore's API) to get pre-signed URLs
2. Uses those signed URLs to PUT/GET/DELETE files from storage (GCS, S3, or custom)
3. Requires `SEMAPHORE_ARTIFACT_TOKEN` and `SEMAPHORE_ORGANIZATION_URL`

## Goal

Add **direct S3 support** that bypasses the Hub, allowing:
- Self-hosted artifact storage using any S3-compatible backend
- Multiple authentication methods (keys, IAM roles, IRSA, SSO)
- Use outside of Semaphore CI (local dev, other CI systems)

---

## Implementation Plan

### Phase 1: Backend Abstraction

**1.1 Create Storage Backend Interface** (`pkg/backend/backend.go`)

```go
type Backend interface {
    Push(ctx context.Context, localPath, remotePath string, force bool) error
    Pull(ctx context.Context, remotePath, localPath string) error
    Yank(ctx context.Context, remotePath string) error
    Exists(ctx context.Context, remotePath string) (bool, error)
}
```

**1.2 Refactor Hub Client**
- Move hub client to implement `Backend` interface
- Extract into `pkg/backend/hub/`

### Phase 2: S3 Backend Implementation

**2.1 New Package** (`pkg/backend/s3/`)

```
pkg/backend/s3/
├── client.go      # S3 client wrapper
├── config.go      # Configuration parsing
├── push.go        # Upload implementation
├── pull.go        # Download implementation
├── yank.go        # Delete implementation
└── auth.go        # Auth method detection
```

**2.2 Dependencies**
```go
// go.mod additions
require (
    github.com/aws/aws-sdk-go-v2 v1.x
    github.com/aws/aws-sdk-go-v2/config v1.x
    github.com/aws/aws-sdk-go-v2/service/s3 v1.x
    github.com/aws/aws-sdk-go-v2/credentials v1.x
)
```

**2.3 Authentication Methods**

| Method | Environment Variables | Use Case |
|--------|----------------------|----------|
| Access Keys | `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` | Simple setup |
| Session Token | + `AWS_SESSION_TOKEN` | Temporary creds |
| IAM Role | (auto-detected) | EC2, ECS |
| IRSA | `AWS_WEB_IDENTITY_TOKEN_FILE`, `AWS_ROLE_ARN` | EKS |
| SSO | `AWS_PROFILE` | Local dev |
| Custom Endpoint | `AWS_ENDPOINT_URL` or `ARTIFACT_S3_ENDPOINT` | MinIO, R2, etc. |

**2.4 Configuration**

New environment variables:
```bash
# Required for S3 mode
ARTIFACT_BACKEND=s3              # Switch from 'hub' (default) to 's3'
ARTIFACT_S3_BUCKET=my-bucket     # S3 bucket name

# Optional
ARTIFACT_S3_REGION=us-east-1     # Default: auto-detect or us-east-1
ARTIFACT_S3_PREFIX=artifacts     # Path prefix in bucket
ARTIFACT_S3_ENDPOINT=            # Custom endpoint for S3-compatible
ARTIFACT_S3_FORCE_PATH_STYLE=    # For MinIO compatibility
```

Config file support (`.artifact.yaml`):
```yaml
backend: s3
s3:
  bucket: my-artifacts
  region: ap-northeast-1
  prefix: ci/artifacts
  endpoint: https://minio.example.com  # optional
  forcePathStyle: true                  # optional
```

### Phase 3: CLI Updates

**3.1 Backend Selection Logic** (`cmd/utils.go`)

```go
func NewBackend() (backend.Backend, error) {
    backendType := os.Getenv("ARTIFACT_BACKEND")
    if backendType == "" {
        backendType = viper.GetString("backend")
    }
    
    switch backendType {
    case "s3":
        return s3backend.NewClient()
    case "hub", "":
        return hub.NewClient()
    default:
        return nil, fmt.Errorf("unknown backend: %s", backendType)
    }
}
```

**3.2 Update Commands**
- Modify `push.go`, `pull.go`, `yank.go` to use `NewBackend()`
- Keep existing path resolution logic (project/workflow/job levels)

### Phase 4: Advanced Features

**4.1 Multipart Upload**
- For files > 100MB, use S3 multipart upload
- Configurable threshold via `ARTIFACT_S3_MULTIPART_THRESHOLD`

**4.2 Transfer Acceleration**
- Support `ARTIFACT_S3_ACCELERATE=true` for S3 Transfer Acceleration

**4.3 Server-Side Encryption**
- `ARTIFACT_S3_SSE=AES256` or `aws:kms`
- `ARTIFACT_S3_KMS_KEY_ID` for KMS encryption

---

## File Changes Summary

| File | Change |
|------|--------|
| `go.mod` | Add AWS SDK v2 dependencies |
| `pkg/backend/backend.go` | **NEW** - Backend interface |
| `pkg/backend/hub/client.go` | **NEW** - Refactored hub client |
| `pkg/backend/s3/*.go` | **NEW** - S3 implementation |
| `cmd/utils.go` | Add `NewBackend()` function |
| `cmd/push.go` | Use backend interface |
| `cmd/pull.go` | Use backend interface |
| `cmd/yank.go` | Use backend interface |
| `pkg/storage/push.go` | Refactor to accept backend |
| `pkg/storage/pull.go` | Refactor to accept backend |
| `pkg/storage/yank.go` | Refactor to accept backend |

---

## Testing Strategy

1. **Unit Tests**: Mock S3 client for backend tests
2. **Integration Tests**: LocalStack or MinIO container
3. **E2E Tests**: Real S3 bucket (optional, CI secrets)

Docker Compose addition:
```yaml
services:
  minio:
    image: minio/minio
    command: server /data
    environment:
      MINIO_ROOT_USER: minioadmin
      MINIO_ROOT_PASSWORD: minioadmin
    ports:
      - "9000:9000"
```

---

## Migration Path

1. Default behavior unchanged (`ARTIFACT_BACKEND` unset = Hub mode)
2. Existing env vars (`SEMAPHORE_*`) still work for Hub mode
3. S3 mode is opt-in via `ARTIFACT_BACKEND=s3`
4. Document both modes in README

---

## Estimated Effort

| Phase | Effort |
|-------|--------|
| Phase 1: Backend Abstraction | 2-3 hours |
| Phase 2: S3 Implementation | 4-6 hours |
| Phase 3: CLI Updates | 1-2 hours |
| Phase 4: Advanced Features | 2-4 hours |
| Testing & Documentation | 2-3 hours |
| **Total** | **11-18 hours** |

---

## Next Steps

1. [ ] Create `pkg/backend/backend.go` interface
2. [ ] Add AWS SDK v2 dependencies
3. [ ] Implement basic S3 push/pull/yank
4. [ ] Add configuration parsing
5. [ ] Update CLI commands
6. [ ] Write tests with MinIO
7. [ ] Update README with S3 documentation
