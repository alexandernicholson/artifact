package backend

import (
	"fmt"
)

// NewBackend creates a new backend based on configuration.
// It determines the backend type from environment variables or config file
// and returns the appropriate implementation.
//
// For hub backend: requires SEMAPHORE_ARTIFACT_TOKEN and SEMAPHORE_ORGANIZATION_URL
// For S3 backend: requires ARTIFACT_S3_BUCKET (and optional region, endpoint, etc.)
func NewBackend() (Backend, error) {
	backendType := GetBackendType()

	switch backendType {
	case BackendTypeHub:
		// Import and create hub backend
		// We use a lazy import approach here to avoid circular dependencies
		return newHubBackend()

	case BackendTypeS3:
		// Import and create S3 backend
		return newS3Backend()

	default:
		return nil, fmt.Errorf("unknown backend type: %s", backendType)
	}
}

// These will be set by init() in the respective backend packages
var newHubBackend func() (Backend, error)
var newS3Backend func() (Backend, error)

// RegisterHubBackend registers the hub backend constructor.
func RegisterHubBackend(fn func() (Backend, error)) {
	newHubBackend = fn
}

// RegisterS3Backend registers the S3 backend constructor.
func RegisterS3Backend(fn func() (Backend, error)) {
	newS3Backend = fn
}
