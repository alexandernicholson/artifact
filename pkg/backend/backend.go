// Package backend provides a unified interface for artifact storage backends.
// This abstraction allows the artifact CLI to work with different storage
// providers (Hub/signed URLs, direct S3, etc.) through a common interface.
package backend

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/viper"
)

// PushOptions contains options for push operations.
type PushOptions struct {
	Force bool // Overwrite existing files
}

// Backend defines the interface for artifact storage operations.
// Implementations must handle their own authentication and connection management.
type Backend interface {
	// Push uploads a local file or directory to the remote storage.
	// localPath is the path to the local file/directory.
	// remotePath is the destination path in storage (already prefixed with artifacts/projects|workflows|jobs/ID/).
	// Returns error if the operation fails or if file exists and force is false.
	Push(ctx context.Context, localPath, remotePath string, opts PushOptions) error

	// Pull downloads a file or directory from remote storage to local filesystem.
	// remotePath is the source path in storage.
	// localPath is the destination path on local filesystem.
	// Returns error if the remote file doesn't exist or operation fails.
	Pull(ctx context.Context, remotePath, localPath string) error

	// Yank deletes a file or directory from remote storage.
	// remotePath is the path to delete in storage.
	// Returns error if the operation fails (not if file doesn't exist).
	Yank(ctx context.Context, remotePath string) error

	// Exists checks if a file exists in remote storage.
	// remotePath is the path to check.
	// Returns true if exists, false otherwise. Error only on operation failure.
	Exists(ctx context.Context, remotePath string) (bool, error)

	// Close releases any resources held by the backend.
	Close() error
}

// BackendType represents the type of storage backend.
type BackendType string

const (
	// BackendTypeHub uses Semaphore Hub for signed URL generation.
	BackendTypeHub BackendType = "hub"

	// BackendTypeS3 uses direct S3 API calls.
	BackendTypeS3 BackendType = "s3"
)

// Config holds common configuration for backends.
type Config struct {
	Type    BackendType
	Verbose bool
}

// GetBackendType determines which backend to use based on environment and config.
// Priority: ARTIFACT_BACKEND env var > config file > default (hub)
func GetBackendType() BackendType {
	// Check environment variable first
	if envBackend := os.Getenv("ARTIFACT_BACKEND"); envBackend != "" {
		switch envBackend {
		case "s3":
			return BackendTypeS3
		case "hub":
			return BackendTypeHub
		default:
			// Unknown backend type, fall through to config/default
		}
	}

	// Check config file
	if configBackend := viper.GetString("backend"); configBackend != "" {
		switch configBackend {
		case "s3":
			return BackendTypeS3
		case "hub":
			return BackendTypeHub
		}
	}

	// Default to hub for backwards compatibility
	return BackendTypeHub
}

// ErrNotFound is returned when a requested artifact does not exist.
type ErrNotFound struct {
	Path string
}

func (e *ErrNotFound) Error() string {
	return fmt.Sprintf("artifact not found: %s", e.Path)
}

// ErrAlreadyExists is returned when trying to push without force and file exists.
type ErrAlreadyExists struct {
	Path string
}

func (e *ErrAlreadyExists) Error() string {
	return fmt.Sprintf("artifact already exists: %s (use --force to overwrite)", e.Path)
}

// ErrPermissionDenied is returned when the backend lacks permission for an operation.
type ErrPermissionDenied struct {
	Operation string
	Path      string
	Reason    string
}

func (e *ErrPermissionDenied) Error() string {
	return fmt.Sprintf("permission denied for %s on %s: %s", e.Operation, e.Path, e.Reason)
}
