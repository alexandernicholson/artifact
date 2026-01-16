// Package s3backend implements the Backend interface using direct S3 API calls.
// This allows artifact storage without requiring Semaphore Hub, enabling
// self-hosted artifact storage with any S3-compatible backend.
package s3backend

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
)

// Config holds S3 backend configuration.
type Config struct {
	// Bucket is the S3 bucket name (required)
	Bucket string

	// Region is the AWS region (optional, auto-detected if not set)
	Region string

	// Endpoint is a custom S3 endpoint for S3-compatible services like MinIO
	Endpoint string

	// ForcePathStyle uses path-style URLs instead of virtual-hosted-style
	// Required for some S3-compatible services like MinIO
	ForcePathStyle bool

	// Prefix is an optional path prefix for all artifacts
	Prefix string
}

// LoadConfig loads S3 configuration from environment variables and config file.
// Environment variables take precedence over config file values.
//
// Environment variables:
//   - ARTIFACT_S3_BUCKET (required)
//   - ARTIFACT_S3_REGION (optional)
//   - ARTIFACT_S3_ENDPOINT (optional)
//   - ARTIFACT_S3_FORCE_PATH_STYLE (optional, "true" to enable)
//   - ARTIFACT_S3_PREFIX (optional)
//
// Config file keys (under 's3' section):
//   - bucket, region, endpoint, forcePathStyle, prefix
func LoadConfig() (*Config, error) {
	cfg := &Config{}

	// Load from environment variables first
	cfg.Bucket = os.Getenv("ARTIFACT_S3_BUCKET")
	cfg.Region = os.Getenv("ARTIFACT_S3_REGION")
	cfg.Endpoint = os.Getenv("ARTIFACT_S3_ENDPOINT")
	cfg.ForcePathStyle = os.Getenv("ARTIFACT_S3_FORCE_PATH_STYLE") == "true"
	cfg.Prefix = os.Getenv("ARTIFACT_S3_PREFIX")

	// Fall back to config file for unset values
	if cfg.Bucket == "" {
		cfg.Bucket = viper.GetString("s3.bucket")
	}
	if cfg.Region == "" {
		cfg.Region = viper.GetString("s3.region")
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = viper.GetString("s3.endpoint")
	}
	if !cfg.ForcePathStyle {
		cfg.ForcePathStyle = viper.GetBool("s3.forcePathStyle")
	}
	if cfg.Prefix == "" {
		cfg.Prefix = viper.GetString("s3.prefix")
	}

	// Validate required fields
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("S3 bucket not configured: set ARTIFACT_S3_BUCKET or s3.bucket in config")
	}

	return cfg, nil
}
