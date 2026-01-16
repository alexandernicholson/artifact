package s3backend

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/semaphoreci/artifact/pkg/backend"
	log "github.com/sirupsen/logrus"
)

func init() {
	backend.RegisterS3Backend(func() (backend.Backend, error) {
		return New()
	})
}

// S3Backend implements the Backend interface using AWS S3.
type S3Backend struct {
	client *s3.Client
	cfg    *Config
}

// New creates a new S3Backend instance.
// It loads configuration from environment/config file and initializes
// the AWS SDK client with automatic credential detection.
func New() (*S3Backend, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	// Build AWS config with automatic credential chain
	awsCfgOpts := []func(*config.LoadOptions) error{}

	// Set region if specified
	if cfg.Region != "" {
		awsCfgOpts = append(awsCfgOpts, config.WithRegion(cfg.Region))
	}

	awsCfg, err := config.LoadDefaultConfig(context.Background(), awsCfgOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client with optional custom endpoint
	s3Opts := []func(*s3.Options){}

	if cfg.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		})
	}

	if cfg.ForcePathStyle {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.UsePathStyle = true
		})
	}

	client := s3.NewFromConfig(awsCfg, s3Opts...)

	log.Debug("S3Backend: Client initialized\n")
	log.Debugf("* Bucket: %s\n", cfg.Bucket)
	log.Debugf("* Region: %s\n", cfg.Region)
	log.Debugf("* Endpoint: %s\n", cfg.Endpoint)

	return &S3Backend{
		client: client,
		cfg:    cfg,
	}, nil
}

// Push uploads a local file or directory to S3.
func (s *S3Backend) Push(ctx context.Context, localPath, remotePath string, opts backend.PushOptions) error {
	log.Debug("S3Backend: Pushing...\n")
	log.Debugf("* Local: %s\n", localPath)
	log.Debugf("* Remote: %s\n", remotePath)
	log.Debugf("* Force: %v\n", opts.Force)

	// Check if source is file or directory
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("failed to stat local path '%s': %w", localPath, err)
	}

	if info.IsDir() {
		return s.pushDirectory(ctx, localPath, remotePath, opts)
	}

	return s.pushFile(ctx, localPath, remotePath, opts)
}

func (s *S3Backend) pushFile(ctx context.Context, localPath, remotePath string, opts backend.PushOptions) error {
	key := s.prefixedKey(remotePath)

	// Check if exists (unless force)
	if !opts.Force {
		exists, err := s.Exists(ctx, remotePath)
		if err != nil {
			return err
		}
		if exists {
			return &backend.ErrAlreadyExists{Path: remotePath}
		}
	}

	// Open local file
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open local file '%s': %w", localPath, err)
	}
	defer file.Close()

	// Upload to S3
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(key),
		Body:   file,
	})
	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	log.Debugf("Uploaded: %s -> s3://%s/%s\n", localPath, s.cfg.Bucket, key)
	return nil
}

func (s *S3Backend) pushDirectory(ctx context.Context, localPath, remotePath string, opts backend.PushOptions) error {
	return filepath.Walk(localPath, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		// Calculate relative path
		relPath, err := filepath.Rel(localPath, filePath)
		if err != nil {
			return err
		}

		// Build remote path
		destPath := path.Join(remotePath, filepath.ToSlash(relPath))

		return s.pushFile(ctx, filePath, destPath, opts)
	})
}

// Pull downloads a file or directory from S3.
func (s *S3Backend) Pull(ctx context.Context, remotePath, localPath string) error {
	log.Debug("S3Backend: Pulling...\n")
	log.Debugf("* Remote: %s\n", remotePath)
	log.Debugf("* Local: %s\n", localPath)

	key := s.prefixedKey(remotePath)

	// List objects with this prefix to handle both files and directories
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.cfg.Bucket),
		Prefix: aws.String(key),
	})

	foundAny := false
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list S3 objects: %w", err)
		}

		for _, obj := range page.Contents {
			foundAny = true
			objKey := aws.ToString(obj.Key)

			// Calculate local destination
			relPath := strings.TrimPrefix(objKey, key)
			destPath := filepath.Join(localPath, relPath)

			if err := s.pullFile(ctx, objKey, destPath); err != nil {
				return err
			}
		}
	}

	if !foundAny {
		return &backend.ErrNotFound{Path: remotePath}
	}

	return nil
}

func (s *S3Backend) pullFile(ctx context.Context, key, localPath string) error {
	// Ensure directory exists
	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory '%s': %w", dir, err)
	}

	// Download from S3
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to download from S3: %w", err)
	}
	defer result.Body.Close()

	// Create local file
	file, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file '%s': %w", localPath, err)
	}
	defer file.Close()

	// Copy content
	if _, err := io.Copy(file, result.Body); err != nil {
		return fmt.Errorf("failed to write to local file: %w", err)
	}

	log.Debugf("Downloaded: s3://%s/%s -> %s\n", s.cfg.Bucket, key, localPath)
	return nil
}

// Yank deletes a file or directory from S3.
func (s *S3Backend) Yank(ctx context.Context, remotePath string) error {
	log.Debug("S3Backend: Yanking...\n")
	log.Debugf("* Remote: %s\n", remotePath)

	key := s.prefixedKey(remotePath)

	// List all objects with this prefix
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.cfg.Bucket),
		Prefix: aws.String(key),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list S3 objects: %w", err)
		}

		for _, obj := range page.Contents {
			_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(s.cfg.Bucket),
				Key:    obj.Key,
			})
			if err != nil {
				return fmt.Errorf("failed to delete S3 object '%s': %w", aws.ToString(obj.Key), err)
			}
			log.Debugf("Deleted: s3://%s/%s\n", s.cfg.Bucket, aws.ToString(obj.Key))
		}
	}

	return nil
}

// Exists checks if a file exists in S3.
func (s *S3Backend) Exists(ctx context.Context, remotePath string) (bool, error) {
	key := s.prefixedKey(remotePath)

	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		// Check if it's a "not found" error
		// AWS SDK v2 doesn't have a typed error, so we check the string
		if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, fmt.Errorf("failed to check S3 object existence: %w", err)
	}

	return true, nil
}

// Close releases any resources. For S3 backend, this is a no-op.
func (s *S3Backend) Close() error {
	return nil
}

// prefixedKey returns the full S3 key with optional prefix.
func (s *S3Backend) prefixedKey(remotePath string) string {
	if s.cfg.Prefix != "" {
		return path.Join(s.cfg.Prefix, remotePath)
	}
	return remotePath
}
