// Package hubbackend implements the Backend interface using Semaphore Hub
// for signed URL generation. This is the default backend that maintains
// backwards compatibility with existing Semaphore CI workflows.
package hubbackend

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/semaphoreci/artifact/pkg/api"
	"github.com/semaphoreci/artifact/pkg/backend"
	"github.com/semaphoreci/artifact/pkg/files"
	"github.com/semaphoreci/artifact/pkg/hub"
	"github.com/semaphoreci/artifact/pkg/storage"
	log "github.com/sirupsen/logrus"
)

func init() {
	backend.RegisterHubBackend(func() (backend.Backend, error) {
		return New()
	})
}

// HubBackend implements the Backend interface using Semaphore Hub.
type HubBackend struct {
	client *hub.Client
}

// New creates a new HubBackend instance.
// Returns an error if the required environment variables are not set.
func New() (*HubBackend, error) {
	client, err := hub.NewClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create hub client: %w", err)
	}
	return &HubBackend{client: client}, nil
}

// Push uploads a local file or directory to remote storage via Hub signed URLs.
func (h *HubBackend) Push(ctx context.Context, localPath, remotePath string, opts backend.PushOptions) error {
	log.Debug("HubBackend: Pushing...\n")
	log.Debugf("* Local: %s\n", localPath)
	log.Debugf("* Remote: %s\n", remotePath)
	log.Debugf("* Force: %v\n", opts.Force)

	// Locate all artifacts (handles both files and directories)
	artifacts, err := locateArtifactsForPush(localPath, remotePath)
	if err != nil {
		return err
	}

	// Determine request type based on force flag
	requestType := hub.GenerateSignedURLsRequestPUSH
	if opts.Force {
		requestType = hub.GenerateSignedURLsRequestPUSHFORCE
	}

	// Get signed URLs from hub
	response, err := h.client.GenerateSignedURLs(api.RemotePaths(artifacts), requestType)
	if err != nil {
		return fmt.Errorf("failed to generate signed URLs: %w", err)
	}

	// Attach URLs to artifacts
	if err := attachURLsToArtifacts(artifacts, response.Urls, opts.Force); err != nil {
		return err
	}

	// Execute the push operations
	if _, err := executePush(artifacts); err != nil {
		return err
	}

	return nil
}

// Pull downloads a file or directory from remote storage via Hub signed URLs.
func (h *HubBackend) Pull(ctx context.Context, remotePath, localPath string, opts backend.PullOptions) error {
	log.Debug("HubBackend: Pulling...\n")
	log.Debugf("* Remote: %s\n", remotePath)
	log.Debugf("* Local: %s\n", localPath)
	log.Debugf("* Force: %v\n", opts.Force)

	// Get signed URLs from hub
	response, err := h.client.GenerateSignedURLs([]string{remotePath}, hub.GenerateSignedURLsRequestPULL)
	if err != nil {
		return fmt.Errorf("failed to generate signed URLs: %w", err)
	}

	if len(response.Urls) == 0 {
		return &backend.ErrNotFound{Path: remotePath}
	}

	// Build artifacts from signed URLs (checks for existing local files)
	artifacts, err := buildArtifactsForPull(response.Urls, remotePath, localPath, opts.Force)
	if err != nil {
		return err
	}

	// Execute the pull operations
	if _, err := executePull(artifacts); err != nil {
		return err
	}

	return nil
}

// Yank deletes a file or directory from remote storage via Hub signed URLs.
func (h *HubBackend) Yank(ctx context.Context, remotePath string) error {
	log.Debug("HubBackend: Yanking...\n")
	log.Debugf("* Remote: %s\n", remotePath)

	// Get signed URLs from hub
	response, err := h.client.GenerateSignedURLs([]string{remotePath}, hub.GenerateSignedURLsRequestYANK)
	if err != nil {
		return fmt.Errorf("failed to generate signed URLs: %w", err)
	}

	// Execute the delete operations
	if err := executeYank(response.Urls); err != nil {
		return err
	}

	return nil
}

// Exists checks if a file exists in remote storage.
func (h *HubBackend) Exists(ctx context.Context, remotePath string) (bool, error) {
	log.Debug("HubBackend: Checking existence...\n")
	log.Debugf("* Remote: %s\n", remotePath)

	// Use PULL request to check if file exists
	response, err := h.client.GenerateSignedURLs([]string{remotePath}, hub.GenerateSignedURLsRequestPULL)
	if err != nil {
		return false, fmt.Errorf("failed to check existence: %w", err)
	}

	return len(response.Urls) > 0, nil
}

// Close releases resources. For Hub backend, this is a no-op.
func (h *HubBackend) Close() error {
	return nil
}

// Helper functions

func locateArtifactsForPush(localPath, remotePath string) ([]*api.Artifact, error) {
	isFile, err := files.IsFileSrc(localPath)
	if err != nil {
		return nil, fmt.Errorf("path '%s' does not exist locally", localPath)
	}

	if isFile {
		return []*api.Artifact{{
			RemotePath: remotePath,
			LocalPath:  localPath,
		}}, nil
	}

	// Walk directory
	var artifacts []*api.Artifact
	err = filepath.Walk(localPath, func(filename string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		name := filepath.ToSlash(filename)
		artifacts = append(artifacts, &api.Artifact{
			RemotePath: path.Join(remotePath, name[len(localPath):]),
			LocalPath:  filename,
		})
		return nil
	})

	if err != nil {
		return nil, err
	}

	return artifacts, nil
}

func attachURLsToArtifacts(artifacts []*api.Artifact, signedURLs []*api.SignedURL, force bool) error {
	// With force: 1 URL per artifact (PUT only)
	// Without force: 2 URLs per artifact (HEAD + PUT)
	expectedURLs := len(artifacts)
	if !force {
		expectedURLs *= 2
	}

	if len(signedURLs) != expectedURLs {
		return fmt.Errorf("unexpected number of signed URLs: got %d, expected %d", len(signedURLs), expectedURLs)
	}

	i := 0
	for _, artifact := range artifacts {
		if force {
			artifact.URLs = []*api.SignedURL{signedURLs[i]}
			i++
		} else {
			artifact.URLs = []*api.SignedURL{signedURLs[i], signedURLs[i+1]}
			i += 2
		}
	}

	return nil
}

func executePush(artifacts []*api.Artifact) (*storage.PushStats, error) {
	client := storage.NewHTTPClient()
	stats := &storage.PushStats{}

	for _, artifact := range artifacts {
		fileInfo, err := os.Stat(artifact.LocalPath)
		if err != nil {
			return nil, fmt.Errorf("failed to stat '%s': %w", artifact.LocalPath, err)
		}

		for _, signedURL := range artifact.URLs {
			if err := signedURL.Follow(client, artifact); err != nil {
				return nil, err
			}
		}

		for _, url := range artifact.URLs {
			if url.Method == "PUT" {
				stats.FileCount++
				stats.TotalSize += fileInfo.Size()
				break
			}
		}
	}

	return stats, nil
}

func buildArtifactsForPull(signedURLs []*api.SignedURL, remotePath, localPath string, force bool) ([]*api.Artifact, error) {
	var artifacts []*api.Artifact

	for _, signedURL := range signedURLs {
		obj, err := signedURL.GetObject()
		if err != nil {
			return nil, err
		}

		destPath := path.Join(localPath, obj[len(remotePath):])

		// Check if local file exists (unless force)
		if !force {
			if _, err := os.Stat(destPath); err == nil {
				return nil, fmt.Errorf("'%s' already exists locally; delete it first, or use --force flag", destPath)
			}
		}

		artifacts = append(artifacts, &api.Artifact{
			RemotePath: obj,
			LocalPath:  destPath,
			URLs:       []*api.SignedURL{signedURL},
		})
	}

	return artifacts, nil
}

func executePull(artifacts []*api.Artifact) (*storage.PullStats, error) {
	client := storage.NewHTTPClient()
	stats := &storage.PullStats{}

	for _, artifact := range artifacts {
		for _, signedURL := range artifact.URLs {
			if err := signedURL.Follow(client, artifact); err != nil {
				return nil, err
			}

			if fileInfo, err := os.Stat(artifact.LocalPath); err == nil {
				stats.FileCount++
				stats.TotalSize += fileInfo.Size()
			}
		}
	}

	return stats, nil
}

func executeYank(signedURLs []*api.SignedURL) error {
	client := storage.NewHTTPClient()

	for _, u := range signedURLs {
		u.Method = "DELETE"
		if err := u.Follow(client, nil); err != nil {
			return err
		}
	}

	return nil
}
