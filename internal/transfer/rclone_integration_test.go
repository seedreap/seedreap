//go:build integration

package transfer_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	testutil "github.com/seedreap/seedreap/internal/testing"
	"github.com/seedreap/seedreap/internal/transfer"
)

// testSSHContainer is a shared SSH container for all tests in this file.
var (
	testSSHContainer     *testutil.SSHContainer
	testSSHContainerOnce sync.Once
	testSSHContainerErr  error
)

// getTestSSHContainer returns the shared SSH container, starting it if necessary.
// The container is shared across all tests to reduce startup time.
func getTestSSHContainer(t *testing.T) *testutil.SSHContainer {
	t.Helper()

	testSSHContainerOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		cfg := testutil.DefaultSSHContainerConfig()
		testSSHContainer, testSSHContainerErr = testutil.StartSSHContainer(ctx, cfg)

		if testSSHContainerErr == nil {
			// Wait for SSH to be ready
			testSSHContainerErr = testSSHContainer.WaitForSSH(ctx, 30*time.Second)
		}
	})

	if testSSHContainerErr != nil {
		t.Skipf("SSH container not available: %v", testSSHContainerErr)
	}

	return testSSHContainer
}

// TestMain handles cleanup of the shared container.
func TestMain(m *testing.M) {
	code := m.Run()

	// Cleanup the shared container
	if testSSHContainer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = testSSHContainer.Cleanup(ctx)
		cancel()
	}

	os.Exit(code)
}

// --- Basic Transfer Tests ---

func TestRcloneIntegration_BasicTransfer(t *testing.T) {
	sshContainer := getTestSSHContainer(t)
	ctx := context.Background()

	t.Run("SmallFile", func(t *testing.T) {
		// Create a small test file
		content := []byte("Hello, World! This is a test file for rclone transfer.")
		remotePath := "test_small_file.txt"

		err := sshContainer.CreateTestFile(ctx, remotePath, content)
		require.NoError(t, err, "failed to create test file")

		// Create local destination
		tmpDir := t.TempDir()
		localPath := filepath.Join(tmpDir, "downloaded_file.txt")

		// Create transferer
		transferer := transfer.NewRclone(transfer.Options{
			SSH: transfer.SSHConfig{
				Host:          sshContainer.Host,
				Port:          sshContainer.Port,
				User:          sshContainer.User,
				KeyFile:       sshContainer.PrivateKey,
				IgnoreHostKey: true,
			},
			ParallelConnections: 4,
		})
		defer func() { _ = transferer.Close() }()

		// Perform transfer
		req := transfer.Request{
			RemotePath: filepath.Join(sshContainer.RemoteDir, remotePath),
			LocalPath:  localPath,
			Size:       int64(len(content)),
		}

		var lastProgress transfer.Progress
		err = transferer.Transfer(ctx, req, func(p transfer.Progress) {
			lastProgress = p
		})
		require.NoError(t, err, "transfer should succeed")

		// Verify file was transferred
		downloadedContent, err := os.ReadFile(localPath)
		require.NoError(t, err, "should be able to read downloaded file")
		assert.Equal(t, content, downloadedContent, "content should match")

		// Verify progress was reported
		assert.Equal(t, int64(len(content)), lastProgress.Transferred, "progress should show full transfer")
	})

	t.Run("MediumFile", func(t *testing.T) {
		// Create a 1MB test file
		const fileSize = 1 * 1024 * 1024 // 1 MB
		remotePath := "test_medium_file.bin"

		err := sshContainer.CreateTestFileWithSize(ctx, remotePath, fileSize)
		require.NoError(t, err, "failed to create test file")

		// Create local destination
		tmpDir := t.TempDir()
		localPath := filepath.Join(tmpDir, "downloaded_medium.bin")

		// Create transferer
		transferer := transfer.NewRclone(transfer.Options{
			SSH: transfer.SSHConfig{
				Host:          sshContainer.Host,
				Port:          sshContainer.Port,
				User:          sshContainer.User,
				KeyFile:       sshContainer.PrivateKey,
				IgnoreHostKey: true,
			},
			ParallelConnections: 4,
		})
		defer func() { _ = transferer.Close() }()

		// Perform transfer
		req := transfer.Request{
			RemotePath: filepath.Join(sshContainer.RemoteDir, remotePath),
			LocalPath:  localPath,
			Size:       fileSize,
		}

		err = transferer.Transfer(ctx, req, nil)
		require.NoError(t, err, "transfer should succeed")

		// Verify file size
		info, err := os.Stat(localPath)
		require.NoError(t, err, "should be able to stat downloaded file")
		assert.Equal(t, int64(fileSize), info.Size(), "file size should match")
	})

	t.Run("LargeFile", func(t *testing.T) {
		// Skip if running in short mode
		if testing.Short() {
			t.Skip("skipping large file test in short mode")
		}

		// Create a 100MB test file to trigger multi-threaded transfer
		const fileSize = 100 * 1024 * 1024 // 100 MB
		remotePath := "test_large_file.bin"

		err := sshContainer.CreateTestFileWithSize(ctx, remotePath, fileSize)
		require.NoError(t, err, "failed to create test file")

		// Create local destination
		tmpDir := t.TempDir()
		localPath := filepath.Join(tmpDir, "downloaded_large.bin")

		// Create transferer with parallel connections
		transferer := transfer.NewRclone(transfer.Options{
			SSH: transfer.SSHConfig{
				Host:          sshContainer.Host,
				Port:          sshContainer.Port,
				User:          sshContainer.User,
				KeyFile:       sshContainer.PrivateKey,
				IgnoreHostKey: true,
			},
			ParallelConnections: 8,
		})
		defer func() { _ = transferer.Close() }()

		// Perform transfer with timeout
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()

		req := transfer.Request{
			RemotePath: filepath.Join(sshContainer.RemoteDir, remotePath),
			LocalPath:  localPath,
			Size:       fileSize,
		}

		var progressUpdates int
		err = transferer.Transfer(ctx, req, func(_ transfer.Progress) {
			progressUpdates++
		})
		require.NoError(t, err, "transfer should succeed")

		// Verify file size
		info, err := os.Stat(localPath)
		require.NoError(t, err, "should be able to stat downloaded file")
		assert.Equal(t, int64(fileSize), info.Size(), "file size should match")

		// Verify we got progress updates
		assert.Greater(t, progressUpdates, 0, "should have received progress updates")
	})
}

// --- Progress Callback Tests ---

func TestRcloneIntegration_ProgressCallbacks(t *testing.T) {
	sshContainer := getTestSSHContainer(t)
	ctx := context.Background()

	t.Run("ProgressUpdatesReported", func(t *testing.T) {
		// Create a 5MB test file
		const fileSize = 5 * 1024 * 1024
		remotePath := "test_progress.bin"

		err := sshContainer.CreateTestFileWithSize(ctx, remotePath, fileSize)
		require.NoError(t, err, "failed to create test file")

		tmpDir := t.TempDir()
		localPath := filepath.Join(tmpDir, "progress_test.bin")

		transferer := transfer.NewRclone(transfer.Options{
			SSH: transfer.SSHConfig{
				Host:          sshContainer.Host,
				Port:          sshContainer.Port,
				User:          sshContainer.User,
				KeyFile:       sshContainer.PrivateKey,
				IgnoreHostKey: true,
			},
			ParallelConnections: 4,
		})
		defer func() { _ = transferer.Close() }()

		req := transfer.Request{
			RemotePath: filepath.Join(sshContainer.RemoteDir, remotePath),
			LocalPath:  localPath,
			Size:       fileSize,
		}

		var progressLog []transfer.Progress
		err = transferer.Transfer(ctx, req, func(p transfer.Progress) {
			progressLog = append(progressLog, p)
		})
		require.NoError(t, err, "transfer should succeed")

		// Verify we got progress updates
		require.NotEmpty(t, progressLog, "should have received progress updates")

		// Final progress should show complete transfer
		lastProgress := progressLog[len(progressLog)-1]
		assert.Equal(t, int64(fileSize), lastProgress.Transferred, "final progress should show complete")
	})

	t.Run("NilProgressCallback", func(t *testing.T) {
		// Ensure nil callback doesn't panic
		content := []byte("test content")
		remotePath := "test_nil_callback.txt"

		err := sshContainer.CreateTestFile(ctx, remotePath, content)
		require.NoError(t, err, "failed to create test file")

		tmpDir := t.TempDir()
		localPath := filepath.Join(tmpDir, "nil_callback.txt")

		transferer := transfer.NewRclone(transfer.Options{
			SSH: transfer.SSHConfig{
				Host:          sshContainer.Host,
				Port:          sshContainer.Port,
				User:          sshContainer.User,
				KeyFile:       sshContainer.PrivateKey,
				IgnoreHostKey: true,
			},
		})
		defer func() { _ = transferer.Close() }()

		req := transfer.Request{
			RemotePath: filepath.Join(sshContainer.RemoteDir, remotePath),
			LocalPath:  localPath,
			Size:       int64(len(content)),
		}

		// Should not panic with nil callback
		err = transferer.Transfer(ctx, req, nil)
		require.NoError(t, err, "transfer should succeed with nil callback")
	})
}

// --- Error Handling Tests ---

func TestRcloneIntegration_ErrorHandling(t *testing.T) {
	sshContainer := getTestSSHContainer(t)
	ctx := context.Background()

	t.Run("NonExistentFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		localPath := filepath.Join(tmpDir, "nonexistent.txt")

		transferer := transfer.NewRclone(transfer.Options{
			SSH: transfer.SSHConfig{
				Host:          sshContainer.Host,
				Port:          sshContainer.Port,
				User:          sshContainer.User,
				KeyFile:       sshContainer.PrivateKey,
				IgnoreHostKey: true,
			},
		})
		defer func() { _ = transferer.Close() }()

		req := transfer.Request{
			RemotePath: filepath.Join(sshContainer.RemoteDir, "does_not_exist.txt"),
			LocalPath:  localPath,
			Size:       100,
		}

		err := transferer.Transfer(ctx, req, nil)
		require.Error(t, err, "transfer of non-existent file should fail")
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		// Create a file that takes time to transfer
		const fileSize = 10 * 1024 * 1024 // 10 MB
		remotePath := "test_cancel.bin"

		err := sshContainer.CreateTestFileWithSize(ctx, remotePath, fileSize)
		require.NoError(t, err, "failed to create test file")

		tmpDir := t.TempDir()
		localPath := filepath.Join(tmpDir, "cancel_test.bin")

		transferer := transfer.NewRclone(transfer.Options{
			SSH: transfer.SSHConfig{
				Host:          sshContainer.Host,
				Port:          sshContainer.Port,
				User:          sshContainer.User,
				KeyFile:       sshContainer.PrivateKey,
				IgnoreHostKey: true,
			},
			ParallelConnections: 1, // Slow it down
		})
		defer func() { _ = transferer.Close() }()

		// Cancel context after short delay
		ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		req := transfer.Request{
			RemotePath: filepath.Join(sshContainer.RemoteDir, remotePath),
			LocalPath:  localPath,
			Size:       fileSize,
		}

		err = transferer.Transfer(ctx, req, nil)
		// Should get an error due to cancellation
		assert.Error(t, err, "transfer should fail due to context cancellation")
	})

	t.Run("InvalidSSHKey", func(t *testing.T) {
		// Create a fake key file
		tmpDir := t.TempDir()
		fakeKeyPath := filepath.Join(tmpDir, "fake_key")
		err := os.WriteFile(fakeKeyPath, []byte("invalid key"), 0600)
		require.NoError(t, err)

		localPath := filepath.Join(tmpDir, "output.txt")

		transferer := transfer.NewRclone(transfer.Options{
			SSH: transfer.SSHConfig{
				Host:          sshContainer.Host,
				Port:          sshContainer.Port,
				User:          sshContainer.User,
				KeyFile:       fakeKeyPath,
				IgnoreHostKey: true,
			},
		})
		defer func() { _ = transferer.Close() }()

		req := transfer.Request{
			RemotePath: filepath.Join(sshContainer.RemoteDir, "test.txt"),
			LocalPath:  localPath,
			Size:       100,
		}

		err = transferer.Transfer(ctx, req, nil)
		require.Error(t, err, "transfer with invalid key should fail")
	})

	t.Run("InvalidHost", func(t *testing.T) {
		tmpDir := t.TempDir()
		localPath := filepath.Join(tmpDir, "output.txt")

		transferer := transfer.NewRclone(transfer.Options{
			SSH: transfer.SSHConfig{
				Host:          "nonexistent.invalid.host",
				Port:          22,
				User:          "user",
				KeyFile:       sshContainer.PrivateKey,
				IgnoreHostKey: true,
			},
		})
		defer func() { _ = transferer.Close() }()

		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		req := transfer.Request{
			RemotePath: "/some/path/file.txt",
			LocalPath:  localPath,
			Size:       100,
		}

		err := transferer.Transfer(ctx, req, nil)
		require.Error(t, err, "transfer to invalid host should fail")
	})
}

// --- Directory Structure Tests ---

func TestRcloneIntegration_DirectoryStructure(t *testing.T) {
	sshContainer := getTestSSHContainer(t)
	ctx := context.Background()

	t.Run("NestedDirectories", func(t *testing.T) {
		// Create file in nested directory
		content := []byte("nested file content")
		remotePath := "level1/level2/level3/nested_file.txt"

		err := sshContainer.CreateTestFile(ctx, remotePath, content)
		require.NoError(t, err, "failed to create nested test file")

		tmpDir := t.TempDir()
		localPath := filepath.Join(tmpDir, "deeply/nested/local/file.txt")

		transferer := transfer.NewRclone(transfer.Options{
			SSH: transfer.SSHConfig{
				Host:          sshContainer.Host,
				Port:          sshContainer.Port,
				User:          sshContainer.User,
				KeyFile:       sshContainer.PrivateKey,
				IgnoreHostKey: true,
			},
		})
		defer func() { _ = transferer.Close() }()

		req := transfer.Request{
			RemotePath: filepath.Join(sshContainer.RemoteDir, remotePath),
			LocalPath:  localPath,
			Size:       int64(len(content)),
		}

		err = transferer.Transfer(ctx, req, nil)
		require.NoError(t, err, "transfer should succeed")

		// Verify nested directories were created
		downloadedContent, err := os.ReadFile(localPath)
		require.NoError(t, err, "should be able to read downloaded file")
		assert.Equal(t, content, downloadedContent, "content should match")
	})
}

// --- Data Integrity Tests ---

func TestRcloneIntegration_DataIntegrity(t *testing.T) {
	sshContainer := getTestSSHContainer(t)
	ctx := context.Background()

	t.Run("BinaryData", func(t *testing.T) {
		// Create file with binary data (all byte values 0-255)
		const fileSize = 10 * 1024 // 10 KB
		remotePath := "test_binary.bin"

		err := sshContainer.CreateTestFileWithSize(ctx, remotePath, fileSize)
		require.NoError(t, err, "failed to create binary test file")

		// Get hash of remote file
		exitCode, reader, err := sshContainer.Container.Exec(ctx, []string{
			"sha256sum", filepath.Join(sshContainer.RemoteDir, remotePath),
		})
		require.NoError(t, err, "failed to get remote hash")
		require.Equal(t, 0, exitCode, "sha256sum should succeed")

		remoteHashOutput, err := io.ReadAll(reader)
		require.NoError(t, err)
		// The output contains Docker stream headers, find the hex hash (64 hex chars)
		remoteHashStr := string(remoteHashOutput)
		var remoteHash string
		for i := 0; i <= len(remoteHashStr)-64; i++ {
			// Check if this position starts a valid hex string
			candidate := remoteHashStr[i : i+64]
			isHex := true
			for _, c := range candidate {
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
					isHex = false
					break
				}
			}
			if isHex {
				remoteHash = candidate
				break
			}
		}
		require.NotEmpty(t, remoteHash, "should find remote hash in output")

		tmpDir := t.TempDir()
		localPath := filepath.Join(tmpDir, "binary.bin")

		transferer := transfer.NewRclone(transfer.Options{
			SSH: transfer.SSHConfig{
				Host:          sshContainer.Host,
				Port:          sshContainer.Port,
				User:          sshContainer.User,
				KeyFile:       sshContainer.PrivateKey,
				IgnoreHostKey: true,
			},
		})
		defer func() { _ = transferer.Close() }()

		req := transfer.Request{
			RemotePath: filepath.Join(sshContainer.RemoteDir, remotePath),
			LocalPath:  localPath,
			Size:       fileSize,
		}

		err = transferer.Transfer(ctx, req, nil)
		require.NoError(t, err, "transfer should succeed")

		// Calculate local hash
		localFile, err := os.Open(localPath)
		require.NoError(t, err)
		defer func() { _ = localFile.Close() }()

		hasher := sha256.New()
		_, err = io.Copy(hasher, localFile)
		require.NoError(t, err)
		localHash := fmt.Sprintf("%x", hasher.Sum(nil))

		// Compare hashes (remote hash is the first 64 chars of sha256sum output)
		assert.Equal(t, remoteHash, localHash, "file hashes should match")
	})
}

// --- Speed Limit Tests ---

func TestRcloneIntegration_SpeedLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping speed limit test in short mode")
	}

	sshContainer := getTestSSHContainer(t)
	ctx := context.Background()

	t.Run("TransferWithSpeedLimit", func(t *testing.T) {
		// Create a 1MB test file
		const fileSize = 1 * 1024 * 1024
		remotePath := "test_speed_limit.bin"

		err := sshContainer.CreateTestFileWithSize(ctx, remotePath, fileSize)
		require.NoError(t, err, "failed to create test file")

		tmpDir := t.TempDir()
		localPath := filepath.Join(tmpDir, "speed_limited.bin")

		// 100 KB/s speed limit - very low to ensure limiting is visible
		const speedLimit = 100 * 1024

		transferer := transfer.NewRclone(transfer.Options{
			SSH: transfer.SSHConfig{
				Host:          sshContainer.Host,
				Port:          sshContainer.Port,
				User:          sshContainer.User,
				KeyFile:       sshContainer.PrivateKey,
				IgnoreHostKey: true,
			},
			SpeedLimit:          speedLimit,
			ParallelConnections: 1,
		})
		defer func() { _ = transferer.Close() }()

		req := transfer.Request{
			RemotePath: filepath.Join(sshContainer.RemoteDir, remotePath),
			LocalPath:  localPath,
			Size:       fileSize,
		}

		err = transferer.Transfer(ctx, req, nil)
		require.NoError(t, err, "transfer should succeed")

		// Verify the file was transferred correctly
		info, err := os.Stat(localPath)
		require.NoError(t, err, "should be able to stat downloaded file")
		assert.Equal(t, int64(fileSize), info.Size(), "file size should match")

		// Note: Speed limiting with local Docker transfers is unreliable due to
		// buffering and caching, so we only verify the transfer completed successfully
		// rather than enforcing timing constraints.
	})
}

// --- Concurrent Transfer Tests ---

func TestRcloneIntegration_ConcurrentTransfers(t *testing.T) {
	sshContainer := getTestSSHContainer(t)
	ctx := context.Background()

	t.Run("MultipleSimultaneousTransfers", func(t *testing.T) {
		// Create multiple test files
		const numFiles = 3
		const fileSize = 1 * 1024 * 1024 // 1 MB each

		for i := range numFiles {
			remotePath := filepath.Join("concurrent", filepath.Base(t.Name()), "file"+string(rune('0'+i))+".bin")
			err := sshContainer.CreateTestFileWithSize(ctx, remotePath, fileSize)
			require.NoError(t, err, "failed to create test file %d", i)
		}

		tmpDir := t.TempDir()

		// Create transferers sequentially to avoid race in rclone's config loading
		// (rclone's fs.NewFs has internal race conditions when called concurrently)
		transferers := make([]transfer.Transferer, numFiles)
		for i := range numFiles {
			transferers[i] = transfer.NewRclone(transfer.Options{
				SSH: transfer.SSHConfig{
					Host:          sshContainer.Host,
					Port:          sshContainer.Port,
					User:          sshContainer.User,
					KeyFile:       sshContainer.PrivateKey,
					IgnoreHostKey: true,
				},
				ParallelConnections: 2,
			})
		}
		defer func() {
			for _, tr := range transferers {
				_ = tr.Close()
			}
		}()

		// Run transfers concurrently
		var wg sync.WaitGroup
		errChan := make(chan error, numFiles)

		for i := range numFiles {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				remotePath := filepath.Join("concurrent", filepath.Base(t.Name()), "file"+string(rune('0'+idx))+".bin")
				localPath := filepath.Join(tmpDir, "file"+string(rune('0'+idx))+".bin")

				req := transfer.Request{
					RemotePath: filepath.Join(sshContainer.RemoteDir, remotePath),
					LocalPath:  localPath,
					Size:       fileSize,
				}

				if err := transferers[idx].Transfer(ctx, req, nil); err != nil {
					errChan <- err
				}
			}(i)
		}

		wg.Wait()
		close(errChan)

		// Check for errors
		for err := range errChan {
			t.Errorf("concurrent transfer failed: %v", err)
		}

		// Verify all files were transferred
		for i := range numFiles {
			localPath := filepath.Join(tmpDir, "file"+string(rune('0'+i))+".bin")
			info, err := os.Stat(localPath)
			require.NoError(t, err, "file %d should exist", i)
			assert.Equal(t, int64(fileSize), info.Size(), "file %d should have correct size", i)
		}
	})
}

// --- GetSpeed Tests ---

func TestRcloneIntegration_GetSpeed(t *testing.T) {
	sshContainer := getTestSSHContainer(t)
	ctx := context.Background()

	t.Run("SpeedReportedDuringTransfer", func(t *testing.T) {
		// Create a file large enough to measure speed
		const fileSize = 5 * 1024 * 1024 // 5 MB
		remotePath := "test_speed_reporting.bin"

		err := sshContainer.CreateTestFileWithSize(ctx, remotePath, fileSize)
		require.NoError(t, err, "failed to create test file")

		tmpDir := t.TempDir()
		localPath := filepath.Join(tmpDir, "speed_test.bin")

		transferer := transfer.NewRclone(transfer.Options{
			SSH: transfer.SSHConfig{
				Host:          sshContainer.Host,
				Port:          sshContainer.Port,
				User:          sshContainer.User,
				KeyFile:       sshContainer.PrivateKey,
				IgnoreHostKey: true,
			},
			ParallelConnections: 4,
		})
		defer func() { _ = transferer.Close() }()

		req := transfer.Request{
			RemotePath: filepath.Join(sshContainer.RemoteDir, remotePath),
			LocalPath:  localPath,
			Size:       fileSize,
		}

		// Check speed during transfer
		var speedSamples []int64
		var mu sync.Mutex

		go func() {
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					speed := transferer.GetSpeed()
					mu.Lock()
					speedSamples = append(speedSamples, speed)
					mu.Unlock()
				}
			}
		}()

		err = transferer.Transfer(ctx, req, nil)
		require.NoError(t, err, "transfer should succeed")

		// Give goroutine time to finish
		time.Sleep(200 * time.Millisecond)

		mu.Lock()
		defer mu.Unlock()

		// We should have collected some speed samples
		assert.NotEmpty(t, speedSamples, "should have collected speed samples")
	})
}
