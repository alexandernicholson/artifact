package cmd

import (
	"context"
	"fmt"

	"github.com/semaphoreci/artifact/pkg/backend"
	errutil "github.com/semaphoreci/artifact/pkg/errors"
)

// getBackend returns the configured storage backend.
// It uses the ARTIFACT_BACKEND env var or config file to determine
// which backend to use (hub or s3).
func getBackend() backend.Backend {
	b, err := backend.NewBackend()
	errutil.Check(err)
	return b
}

// getContext returns a context for backend operations.
// Currently returns a background context, but can be extended
// to support timeouts and cancellation.
func getContext() context.Context {
	return context.Background()
}

// formatBytes converts bytes to human readable format
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// pluralize returns singular or plural form based on count
func pluralize(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}