package s3backend

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"github.com/semaphoreci/artifact/pkg/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestS3Backend creates an S3Backend connected to a fake S3 server for testing
func createTestS3Backend(t *testing.T) (*S3Backend, *httptest.Server, func()) {
	// Create fake S3 backend
	faker := gofakes3.New(s3mem.New())
	server := httptest.NewServer(faker.Server())

	// Create bucket
	awsCfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	require.NoError(t, err)

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	// Create test bucket
	_, err = client.CreateBucket(context.Background(), &s3.CreateBucketInput{
		Bucket: aws.String("test-bucket"),
	})
	require.NoError(t, err)

	// Create S3Backend with the test client
	s3Backend := &S3Backend{
		client: client,
		cfg: &Config{
			Bucket:         "test-bucket",
			Region:         "us-east-1",
			Endpoint:       server.URL,
			ForcePathStyle: true,
		},
	}

	cleanup := func() {
		server.Close()
	}

	return s3Backend, server, cleanup
}

func TestS3Backend_Push_SingleFile(t *testing.T) {
	s3Backend, _, cleanup := createTestS3Backend(t)
	defer cleanup()

	// Create temp file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("hello world"), 0644)
	require.NoError(t, err)

	// Push file
	ctx := context.Background()
	err = s3Backend.Push(ctx, testFile, "artifacts/projects/123/test.txt", backend.PushOptions{})
	assert.NoError(t, err)

	// Verify it exists
	exists, err := s3Backend.Exists(ctx, "artifacts/projects/123/test.txt")
	assert.NoError(t, err)
	assert.True(t, exists)
}

func TestS3Backend_Push_Directory(t *testing.T) {
	s3Backend, _, cleanup := createTestS3Backend(t)
	defer cleanup()

	// Create temp directory with files
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	err := os.MkdirAll(subDir, 0755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content1"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(subDir, "file2.txt"), []byte("content2"), 0644)
	require.NoError(t, err)

	// Push directory
	ctx := context.Background()
	err = s3Backend.Push(ctx, tmpDir, "artifacts/jobs/456/data", backend.PushOptions{})
	assert.NoError(t, err)

	// Verify files exist
	exists, err := s3Backend.Exists(ctx, "artifacts/jobs/456/data/file1.txt")
	assert.NoError(t, err)
	assert.True(t, exists)

	exists, err = s3Backend.Exists(ctx, "artifacts/jobs/456/data/subdir/file2.txt")
	assert.NoError(t, err)
	assert.True(t, exists)
}

func TestS3Backend_Push_AlreadyExists(t *testing.T) {
	s3Backend, _, cleanup := createTestS3Backend(t)
	defer cleanup()

	// Create and push file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("original"), 0644)
	require.NoError(t, err)

	ctx := context.Background()
	err = s3Backend.Push(ctx, testFile, "artifacts/projects/123/test.txt", backend.PushOptions{})
	require.NoError(t, err)

	// Try to push again without force - should fail
	err = s3Backend.Push(ctx, testFile, "artifacts/projects/123/test.txt", backend.PushOptions{Force: false})
	assert.Error(t, err)
	assert.IsType(t, &backend.ErrAlreadyExists{}, err)

	// Push with force - should succeed
	err = s3Backend.Push(ctx, testFile, "artifacts/projects/123/test.txt", backend.PushOptions{Force: true})
	assert.NoError(t, err)
}

func TestS3Backend_Pull_SingleFile(t *testing.T) {
	s3Backend, _, cleanup := createTestS3Backend(t)
	defer cleanup()

	// Push a file first
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "source.txt")
	err := os.WriteFile(srcFile, []byte("test content"), 0644)
	require.NoError(t, err)

	ctx := context.Background()
	err = s3Backend.Push(ctx, srcFile, "artifacts/projects/123/source.txt", backend.PushOptions{})
	require.NoError(t, err)

	// Pull file
	dstFile := filepath.Join(tmpDir, "destination.txt")
	err = s3Backend.Pull(ctx, "artifacts/projects/123/source.txt", dstFile, backend.PullOptions{})
	assert.NoError(t, err)

	// Verify content
	content, err := os.ReadFile(dstFile)
	assert.NoError(t, err)
	assert.Equal(t, "test content", string(content))
}

func TestS3Backend_Pull_NotFound(t *testing.T) {
	s3Backend, _, cleanup := createTestS3Backend(t)
	defer cleanup()

	ctx := context.Background()
	tmpDir := t.TempDir()
	dstFile := filepath.Join(tmpDir, "nonexistent.txt")

	err := s3Backend.Pull(ctx, "artifacts/projects/123/nonexistent.txt", dstFile, backend.PullOptions{})
	assert.Error(t, err)
	assert.IsType(t, &backend.ErrNotFound{}, err)
}

func TestS3Backend_Yank(t *testing.T) {
	s3Backend, _, cleanup := createTestS3Backend(t)
	defer cleanup()

	// Push a file first
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("to be deleted"), 0644)
	require.NoError(t, err)

	ctx := context.Background()
	err = s3Backend.Push(ctx, testFile, "artifacts/jobs/789/test.txt", backend.PushOptions{})
	require.NoError(t, err)

	// Verify it exists
	exists, err := s3Backend.Exists(ctx, "artifacts/jobs/789/test.txt")
	require.NoError(t, err)
	require.True(t, exists)

	// Yank it
	err = s3Backend.Yank(ctx, "artifacts/jobs/789/test.txt")
	assert.NoError(t, err)

	// Verify it's gone
	exists, err = s3Backend.Exists(ctx, "artifacts/jobs/789/test.txt")
	assert.NoError(t, err)
	assert.False(t, exists)
}

func TestS3Backend_Exists(t *testing.T) {
	s3Backend, _, cleanup := createTestS3Backend(t)
	defer cleanup()

	ctx := context.Background()

	// File doesn't exist
	exists, err := s3Backend.Exists(ctx, "artifacts/projects/123/nonexistent.txt")
	assert.NoError(t, err)
	assert.False(t, exists)

	// Push a file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	err = os.WriteFile(testFile, []byte("exists"), 0644)
	require.NoError(t, err)

	err = s3Backend.Push(ctx, testFile, "artifacts/projects/123/test.txt", backend.PushOptions{})
	require.NoError(t, err)

	// Now it exists
	exists, err = s3Backend.Exists(ctx, "artifacts/projects/123/test.txt")
	assert.NoError(t, err)
	assert.True(t, exists)
}
