package filesync_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seedreap/seedreap/internal/download"
	"github.com/seedreap/seedreap/internal/filesync"
	testutil "github.com/seedreap/seedreap/internal/testing"
	"github.com/seedreap/seedreap/internal/transfer"
)

// --- FileProgress Tests ---

func TestFileProgress(t *testing.T) {
	t.Run("Progress", func(t *testing.T) {
		fp := &filesync.FileProgress{
			Path:        "file.mkv",
			Size:        1024 * 1024,
			Transferred: 512 * 1024,
			BytesPerSec: 100 * 1024,
			Status:      filesync.FileStatusSyncing,
		}

		transferred, bytesPerSec := fp.Progress()
		assert.Equal(t, int64(512*1024), transferred)
		assert.Equal(t, int64(100*1024), bytesPerSec)
	})

	t.Run("SetProgress", func(t *testing.T) {
		fp := &filesync.FileProgress{
			Path: "file.mkv",
			Size: 1024 * 1024,
		}

		fp.SetProgress(256*1024, 50*1024)

		transferred, bytesPerSec := fp.Progress()
		assert.Equal(t, int64(256*1024), transferred)
		assert.Equal(t, int64(50*1024), bytesPerSec)
	})

	t.Run("GetStatus", func(t *testing.T) {
		fp := &filesync.FileProgress{
			Status: filesync.FileStatusComplete,
		}

		assert.Equal(t, filesync.FileStatusComplete, fp.GetStatus())
	})

	t.Run("Snapshot", func(t *testing.T) {
		fp := &filesync.FileProgress{
			Path:        "test/file.mkv",
			Size:        2 * 1024 * 1024,
			Transferred: 1024 * 1024,
			Status:      filesync.FileStatusSyncing,
			BytesPerSec: 200 * 1024,
		}

		snap := fp.Snapshot()

		assert.Equal(t, "test/file.mkv", snap.Path)
		assert.Equal(t, int64(2*1024*1024), snap.Size)
		assert.Equal(t, int64(1024*1024), snap.Transferred)
		assert.Equal(t, filesync.FileStatusSyncing, snap.Status)
		assert.Equal(t, int64(200*1024), snap.BytesPerSec)
	})

	t.Run("ConcurrentAccess", func(_ *testing.T) {
		fp := &filesync.FileProgress{
			Path:   "file.mkv",
			Size:   10 * 1024 * 1024,
			Status: filesync.FileStatusPending,
		}

		var wg sync.WaitGroup
		const goroutines = 10
		const iterations = 100

		for i := range goroutines {
			wg.Go(func() {
				for j := range iterations {
					fp.SetProgress(int64(i*iterations+j), int64(i*1024))
					_, _ = fp.Progress()
					_ = fp.GetStatus()
					_ = fp.Snapshot()
				}
			})
		}

		wg.Wait()
	})
}

// --- SyncJob Tests ---

func TestSyncJob(t *testing.T) {
	t.Run("GetProgress", func(t *testing.T) {
		job := createTestJob()

		completedSize, status := job.GetProgress()
		assert.Equal(t, int64(0), completedSize)
		assert.Equal(t, filesync.FileStatusPending, status)

		// Simulate file progress
		job.Files[0].SetProgress(256*1024, 100*1024)
		job.Files[1].SetProgress(128*1024, 50*1024)

		completedSize, _ = job.GetProgress()
		assert.Equal(t, int64(256*1024+128*1024), completedSize)
	})

	t.Run("Cancel", func(t *testing.T) {
		job := createTestJob()

		assert.False(t, job.IsCancelled())

		job.Cancel()

		assert.True(t, job.IsCancelled())
		_, status := job.GetProgress()
		assert.Equal(t, filesync.FileStatusError, status)
	})

	t.Run("Context", func(t *testing.T) {
		job := createTestJob()

		ctx := job.Context()
		require.NotNil(t, ctx)

		// Context should be cancellable via Cancel()
		job.Cancel()

		select {
		case <-ctx.Done():
			// Expected
		case <-time.After(100 * time.Millisecond):
			t.Fatal("context should be cancelled after Cancel()")
		}
	})

	t.Run("UpdateDestination", func(t *testing.T) {
		job := createTestJob()

		assert.Equal(t, "tv", job.Category)
		assert.Equal(t, "/downloads/tv", job.FinalPath)

		job.UpdateDestination("/downloads/movies", "movies")

		assert.Equal(t, "movies", job.Category)
		assert.Equal(t, "/downloads/movies", job.GetFinalPath())
	})

	t.Run("Snapshot", func(t *testing.T) {
		job := createTestJob()
		job.Files[0].SetProgress(256*1024, 100*1024)
		job.Files[0].Status = filesync.FileStatusSyncing

		snap := job.Snapshot()

		assert.Equal(t, "test-job-TestTorrent", snap.ID)
		assert.Equal(t, "TestTorrent", snap.Name)
		assert.Equal(t, "test-downloader", snap.Downloader)
		assert.Equal(t, "tv", snap.Category)
		assert.Equal(t, filesync.FileStatusPending, snap.Status)
		assert.Len(t, snap.Files, 2)
		assert.Equal(t, int64(256*1024), snap.CompletedSize)
		assert.Equal(t, int64(100*1024), snap.BytesPerSec) // From syncing file
	})

	t.Run("ConcurrentAccess", func(_ *testing.T) {
		job := createTestJob()

		var wg sync.WaitGroup
		const goroutines = 10
		const iterations = 100

		for range goroutines {
			wg.Go(func() {
				for range iterations {
					_, _ = job.GetProgress()
					_ = job.IsCancelled()
					_ = job.GetFinalPath()
					_ = job.Snapshot()
				}
			})
		}

		wg.Wait()
	})
}

// --- Syncer Tests ---

func TestSyncer(t *testing.T) {
	t.Run("New", func(t *testing.T) {
		t.Run("DefaultOptions", func(t *testing.T) {
			syncer := filesync.New("/syncing")
			require.NotNil(t, syncer)
		})

		t.Run("WithOptions", func(t *testing.T) {
			mockTransfer := testutil.NewMockTransferer()
			syncer := filesync.New(
				"/syncing",
				filesync.WithMaxConcurrent(4),
				filesync.WithTransferer(mockTransfer),
			)
			require.NotNil(t, syncer)
		})
	})

	t.Run("CreateJob", func(t *testing.T) {
		t.Run("BasicCreation", func(t *testing.T) {
			tmpDir := t.TempDir()
			syncer := filesync.New(filepath.Join(tmpDir, "syncing"))

			dl := createTestDownload("hash1", "TestTorrent", "tv")

			job := syncer.CreateJob(dl, "test-downloader", filepath.Join(tmpDir, "downloads/tv"))

			assert.Equal(t, "hash1", job.ID)
			assert.Equal(t, "TestTorrent", job.Name)
			assert.Equal(t, "test-downloader", job.Downloader)
			assert.Equal(t, "tv", job.Category)
			assert.Len(t, job.Files, 2) // 2 files with priority > 0
			assert.Equal(t, int64(1024*1024), job.TotalSize)
			assert.Equal(t, filesync.FileStatusPending, job.Status)
		})

		t.Run("ReturnsExistingJob", func(t *testing.T) {
			tmpDir := t.TempDir()
			syncer := filesync.New(filepath.Join(tmpDir, "syncing"))

			dl := createTestDownload("hash1", "TestTorrent", "tv")

			job1 := syncer.CreateJob(dl, "test-downloader", filepath.Join(tmpDir, "downloads/tv"))
			job2 := syncer.CreateJob(dl, "test-downloader", filepath.Join(tmpDir, "downloads/tv"))

			assert.Same(t, job1, job2, "should return the same job instance")
		})

		t.Run("SkipsPriorityZeroFiles", func(t *testing.T) {
			tmpDir := t.TempDir()
			syncer := filesync.New(filepath.Join(tmpDir, "syncing"))

			dl := &download.Download{
				ID:       "hash1",
				Name:     "TestTorrent",
				Category: "tv",
				SavePath: "/remote/downloads",
				Files: []download.File{
					{Path: "TestTorrent/file1.mkv", Size: 512 * 1024, Priority: 1},
					{Path: "TestTorrent/file2.mkv", Size: 512 * 1024, Priority: 0}, // Deselected
				},
			}

			job := syncer.CreateJob(dl, "test-downloader", filepath.Join(tmpDir, "downloads/tv"))

			assert.Len(t, job.Files, 1)
			assert.Equal(t, int64(512*1024), job.TotalSize)
		})

		t.Run("MarksExistingFilesAsSkipped", func(t *testing.T) {
			tmpDir := t.TempDir()
			syncer := filesync.New(filepath.Join(tmpDir, "syncing"))
			finalPath := filepath.Join(tmpDir, "downloads/tv")

			dl := createTestDownload("hash1", "TestTorrent", "tv")

			// Pre-create the file at final destination with correct size
			existingPath := filepath.Join(finalPath, "TestTorrent/file1.mkv")
			require.NoError(t, os.MkdirAll(filepath.Dir(existingPath), 0750))
			require.NoError(t, os.WriteFile(existingPath, make([]byte, 512*1024), 0600))

			job := syncer.CreateJob(dl, "test-downloader", finalPath)

			// First file should be skipped (exists)
			assert.Equal(t, filesync.FileStatusSkipped, job.Files[0].Status)
			assert.Equal(t, int64(512*1024), job.Files[0].Transferred)

			// Second file should be pending
			assert.Equal(t, filesync.FileStatusPending, job.Files[1].Status)
		})
	})

	t.Run("GetJob", func(t *testing.T) {
		tmpDir := t.TempDir()
		syncer := filesync.New(filepath.Join(tmpDir, "syncing"))

		dl := createTestDownload("hash1", "TestTorrent", "tv")
		syncer.CreateJob(dl, "test-downloader", filepath.Join(tmpDir, "downloads/tv"))

		t.Run("Found", func(t *testing.T) {
			job, ok := syncer.GetJob("hash1")
			assert.True(t, ok)
			assert.NotNil(t, job)
			assert.Equal(t, "hash1", job.ID)
		})

		t.Run("NotFound", func(t *testing.T) {
			job, ok := syncer.GetJob("nonexistent")
			assert.False(t, ok)
			assert.Nil(t, job)
		})
	})

	t.Run("GetAllJobs", func(t *testing.T) {
		tmpDir := t.TempDir()
		syncer := filesync.New(filepath.Join(tmpDir, "syncing"))

		dl1 := createTestDownload("hash1", "Torrent1", "tv")
		dl2 := createTestDownload("hash2", "Torrent2", "movies")

		syncer.CreateJob(dl1, "test-downloader", filepath.Join(tmpDir, "downloads/tv"))
		syncer.CreateJob(dl2, "test-downloader", filepath.Join(tmpDir, "downloads/movies"))

		jobs := syncer.GetAllJobs()
		assert.Len(t, jobs, 2)
	})

	t.Run("RemoveJob", func(t *testing.T) {
		tmpDir := t.TempDir()
		syncer := filesync.New(filepath.Join(tmpDir, "syncing"))

		dl := createTestDownload("hash1", "TestTorrent", "tv")
		syncer.CreateJob(dl, "test-downloader", filepath.Join(tmpDir, "downloads/tv"))

		_, ok := syncer.GetJob("hash1")
		require.True(t, ok)

		syncer.RemoveJob("hash1")

		_, ok = syncer.GetJob("hash1")
		assert.False(t, ok)
	})

	t.Run("CancelJob", func(t *testing.T) {
		t.Run("ExistingJob", func(t *testing.T) {
			tmpDir := t.TempDir()
			syncingPath := filepath.Join(tmpDir, "syncing")
			syncer := filesync.New(syncingPath)

			dl := createTestDownload("hash1", "TestTorrent", "tv")
			job := syncer.CreateJob(dl, "test-downloader", filepath.Join(tmpDir, "downloads/tv"))

			// Create staging directory to verify cleanup
			require.NoError(t, os.MkdirAll(job.LocalBase, 0750))
			require.NoError(t, os.WriteFile(
				filepath.Join(job.LocalBase, "file.mkv"),
				[]byte("test"),
				0600,
			))

			err := syncer.CancelJob("hash1")
			require.NoError(t, err)

			assert.True(t, job.IsCancelled())

			// Staging directory should be cleaned up
			_, err = os.Stat(job.LocalBase)
			assert.True(t, os.IsNotExist(err))
		})

		t.Run("NonexistentJob", func(t *testing.T) {
			syncer := filesync.New("/syncing")

			err := syncer.CancelJob("nonexistent")
			assert.NoError(t, err, "should not error for nonexistent job")
		})
	})
}

// --- SpeedHistory Tests ---

func TestSpeedHistory(t *testing.T) {
	t.Run("RecordSpeed", func(t *testing.T) {
		syncer := filesync.New("/syncing")

		syncer.RecordSpeed(100 * 1024)
		syncer.RecordSpeed(200 * 1024)
		syncer.RecordSpeed(150 * 1024)

		history := syncer.GetSpeedHistory()
		assert.Len(t, history, 3)
		assert.Equal(t, int64(100*1024), history[0].Speed)
		assert.Equal(t, int64(200*1024), history[1].Speed)
		assert.Equal(t, int64(150*1024), history[2].Speed)
	})

	t.Run("TrimsToMaxSamples", func(t *testing.T) {
		syncer := filesync.New("/syncing")

		// Record more than max samples (100)
		for i := range 150 {
			syncer.RecordSpeed(int64(i * 1024))
		}

		history := syncer.GetSpeedHistory()
		assert.Len(t, history, 100)

		// Should keep the latest samples
		assert.Equal(t, int64(50*1024), history[0].Speed) // First kept sample
	})

	t.Run("GetSpeedHistoryReturnsCopy", func(t *testing.T) {
		syncer := filesync.New("/syncing")

		syncer.RecordSpeed(100 * 1024)

		history1 := syncer.GetSpeedHistory()
		history2 := syncer.GetSpeedHistory()

		// Modify one copy
		history1[0].Speed = 999

		// Other copy should be unchanged
		assert.NotEqual(t, history1[0].Speed, history2[0].Speed)
	})
}

// --- GetAggregateSpeed Tests ---

func TestGetAggregateSpeed(t *testing.T) {
	t.Run("WithTransferer", func(t *testing.T) {
		mockTransfer := testutil.NewMockTransferer()
		mockTransfer.SetSpeed(500 * 1024)

		syncer := filesync.New("/syncing", filesync.WithTransferer(mockTransfer))

		speed := syncer.GetAggregateSpeed()
		assert.Equal(t, int64(500*1024), speed)
	})

	t.Run("WithoutTransferer", func(t *testing.T) {
		syncer := filesync.New("/syncing")

		speed := syncer.GetAggregateSpeed()
		assert.Equal(t, int64(0), speed)
	})
}

// --- SyncFile Tests ---

func TestSyncFile(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		tmpDir := t.TempDir()
		mockTransfer := testutil.NewMockTransferer()
		mockDL := testutil.NewMockDownloader("test-downloader")

		syncer := filesync.New(
			filepath.Join(tmpDir, "syncing"),
			filesync.WithTransferer(mockTransfer),
			filesync.WithMaxConcurrent(1),
		)

		dl := createTestDownload("hash1", "TestTorrent", "tv")
		job := syncer.CreateJob(dl, "test-downloader", filepath.Join(tmpDir, "downloads/tv"))

		err := syncer.SyncFile(context.Background(), mockDL, job, job.Files[0])
		require.NoError(t, err)

		assert.Equal(t, filesync.FileStatusComplete, job.Files[0].GetStatus())
		assert.Equal(t, int64(512*1024), job.Files[0].Transferred)

		// Verify file was created
		_, err = os.Stat(job.Files[0].LocalPath)
		assert.NoError(t, err)
	})

	t.Run("SkipsExistingFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		mockTransfer := testutil.NewMockTransferer()
		mockDL := testutil.NewMockDownloader("test-downloader")

		syncer := filesync.New(
			filepath.Join(tmpDir, "syncing"),
			filesync.WithTransferer(mockTransfer),
			filesync.WithMaxConcurrent(1),
		)

		dl := createTestDownload("hash1", "TestTorrent", "tv")
		job := syncer.CreateJob(dl, "test-downloader", filepath.Join(tmpDir, "downloads/tv"))

		// Pre-create the file at staging location with correct size
		require.NoError(t, os.MkdirAll(filepath.Dir(job.Files[0].LocalPath), 0750))
		require.NoError(t, os.WriteFile(job.Files[0].LocalPath, make([]byte, 512*1024), 0600))

		err := syncer.SyncFile(context.Background(), mockDL, job, job.Files[0])
		require.NoError(t, err)

		assert.Equal(t, filesync.FileStatusSkipped, job.Files[0].GetStatus())

		// Verify no transfer was attempted
		assert.Empty(t, mockTransfer.GetTransferCalls())
	})

	t.Run("TransferError", func(t *testing.T) {
		tmpDir := t.TempDir()
		mockTransfer := testutil.NewMockTransferer()
		mockDL := testutil.NewMockDownloader("test-downloader")

		mockTransfer.OnTransfer = func(
			_ context.Context, _ transfer.Request, _ transfer.ProgressFunc,
		) error {
			return assert.AnError
		}

		syncer := filesync.New(
			filepath.Join(tmpDir, "syncing"),
			filesync.WithTransferer(mockTransfer),
			filesync.WithMaxConcurrent(1),
		)

		dl := createTestDownload("hash1", "TestTorrent", "tv")
		job := syncer.CreateJob(dl, "test-downloader", filepath.Join(tmpDir, "downloads/tv"))

		err := syncer.SyncFile(context.Background(), mockDL, job, job.Files[0])
		require.Error(t, err)
		assert.Contains(t, err.Error(), "transfer failed")

		assert.Equal(t, filesync.FileStatusError, job.Files[0].GetStatus())
	})

	t.Run("ContextCancelled", func(t *testing.T) {
		tmpDir := t.TempDir()
		mockTransfer := testutil.NewMockTransferer()
		mockDL := testutil.NewMockDownloader("test-downloader")

		// Configure mock transfer to check context and return cancel error
		mockTransfer.OnTransfer = func(
			ctx context.Context, _ transfer.Request, _ transfer.ProgressFunc,
		) error {
			return ctx.Err()
		}

		syncer := filesync.New(
			filepath.Join(tmpDir, "syncing"),
			filesync.WithTransferer(mockTransfer),
			filesync.WithMaxConcurrent(1),
		)

		dl := createTestDownload("hash1", "TestTorrent", "tv")
		job := syncer.CreateJob(dl, "test-downloader", filepath.Join(tmpDir, "downloads/tv"))

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err := syncer.SyncFile(ctx, mockDL, job, job.Files[0])
		require.Error(t, err)
		// Error may be from semaphore acquisition OR from transfer, depending on race
		assert.True(t, errors.Is(err, context.Canceled) ||
			strings.Contains(err.Error(), "context canceled"),
			"error should be related to cancellation")
	})

	t.Run("NoTransfererConfigured", func(t *testing.T) {
		tmpDir := t.TempDir()
		mockDL := testutil.NewMockDownloader("test-downloader")

		syncer := filesync.New(
			filepath.Join(tmpDir, "syncing"),
			filesync.WithMaxConcurrent(1),
		)

		dl := createTestDownload("hash1", "TestTorrent", "tv")
		job := syncer.CreateJob(dl, "test-downloader", filepath.Join(tmpDir, "downloads/tv"))

		err := syncer.SyncFile(context.Background(), mockDL, job, job.Files[0])
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no transfer backend configured")
	})

	t.Run("CallsFileCompleteCallback", func(t *testing.T) {
		tmpDir := t.TempDir()
		mockTransfer := testutil.NewMockTransferer()
		mockDL := testutil.NewMockDownloader("test-downloader")

		var callbackJob *filesync.SyncJob
		var callbackFile *filesync.FileProgress

		syncer := filesync.New(
			filepath.Join(tmpDir, "syncing"),
			filesync.WithTransferer(mockTransfer),
			filesync.WithMaxConcurrent(1),
			filesync.WithOnFileComplete(func(job *filesync.SyncJob, file *filesync.FileProgress) {
				callbackJob = job
				callbackFile = file
			}),
		)

		dl := createTestDownload("hash1", "TestTorrent", "tv")
		job := syncer.CreateJob(dl, "test-downloader", filepath.Join(tmpDir, "downloads/tv"))

		err := syncer.SyncFile(context.Background(), mockDL, job, job.Files[0])
		require.NoError(t, err)

		assert.Same(t, job, callbackJob)
		assert.Same(t, job.Files[0], callbackFile)
	})
}

// --- SyncJob (method) Tests ---

func TestSyncerSyncJob(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		tmpDir := t.TempDir()
		mockTransfer := testutil.NewMockTransferer()
		mockDL := testutil.NewMockDownloader("test-downloader")

		syncer := filesync.New(
			filepath.Join(tmpDir, "syncing"),
			filesync.WithTransferer(mockTransfer),
			filesync.WithMaxConcurrent(2),
		)

		dl := createTestDownload("hash1", "TestTorrent", "tv")
		mockDL.AddDownload(dl, dl.Files)

		job := syncer.CreateJob(dl, "test-downloader", filepath.Join(tmpDir, "downloads/tv"))

		err := syncer.SyncJob(context.Background(), mockDL, job)
		require.NoError(t, err)

		_, status := job.GetProgress()
		assert.Equal(t, filesync.FileStatusComplete, status)
	})

	t.Run("JobAlreadyCancelled", func(t *testing.T) {
		tmpDir := t.TempDir()
		mockTransfer := testutil.NewMockTransferer()
		mockDL := testutil.NewMockDownloader("test-downloader")

		syncer := filesync.New(
			filepath.Join(tmpDir, "syncing"),
			filesync.WithTransferer(mockTransfer),
		)

		dl := createTestDownload("hash1", "TestTorrent", "tv")
		job := syncer.CreateJob(dl, "test-downloader", filepath.Join(tmpDir, "downloads/tv"))

		job.Cancel()

		err := syncer.SyncJob(context.Background(), mockDL, job)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("CallsJobCompleteCallback", func(t *testing.T) {
		tmpDir := t.TempDir()
		mockTransfer := testutil.NewMockTransferer()
		mockDL := testutil.NewMockDownloader("test-downloader")

		var callbackJob *filesync.SyncJob

		syncer := filesync.New(
			filepath.Join(tmpDir, "syncing"),
			filesync.WithTransferer(mockTransfer),
			filesync.WithMaxConcurrent(2),
			filesync.WithOnJobComplete(func(job *filesync.SyncJob) {
				callbackJob = job
			}),
		)

		dl := createTestDownload("hash1", "TestTorrent", "tv")
		mockDL.AddDownload(dl, dl.Files)

		job := syncer.CreateJob(dl, "test-downloader", filepath.Join(tmpDir, "downloads/tv"))

		err := syncer.SyncJob(context.Background(), mockDL, job)
		require.NoError(t, err)

		assert.Same(t, job, callbackJob)
	})

	t.Run("SkipsIncompleteFilesInDownloader", func(t *testing.T) {
		tmpDir := t.TempDir()
		mockTransfer := testutil.NewMockTransferer()
		mockDL := testutil.NewMockDownloader("test-downloader")

		syncer := filesync.New(
			filepath.Join(tmpDir, "syncing"),
			filesync.WithTransferer(mockTransfer),
			filesync.WithMaxConcurrent(2),
		)

		// Create download with one complete and one incomplete file
		dl := &download.Download{
			ID:       "hash1",
			Name:     "TestTorrent",
			Hash:     "hash1",
			Category: "tv",
			SavePath: "/remote/downloads",
			Files: []download.File{
				{
					Path:     "TestTorrent/file1.mkv",
					Size:     512 * 1024,
					State:    download.FileStateComplete,
					Priority: 1,
				},
				{
					Path:     "TestTorrent/file2.mkv",
					Size:     512 * 1024,
					State:    download.FileStateDownloading, // Not complete
					Priority: 1,
				},
			},
		}
		mockDL.AddDownload(dl, dl.Files)

		job := syncer.CreateJob(dl, "test-downloader", filepath.Join(tmpDir, "downloads/tv"))

		err := syncer.SyncJob(context.Background(), mockDL, job)
		require.NoError(t, err)

		// Job should be pending (not all files complete)
		_, status := job.GetProgress()
		assert.Equal(t, filesync.FileStatusPending, status)

		// Only first file should be complete
		assert.Equal(t, filesync.FileStatusComplete, job.Files[0].GetStatus())
		assert.Equal(t, filesync.FileStatusPending, job.Files[1].GetStatus())
	})
}

// --- MoveToFinal Tests ---

func TestMoveToFinal(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		tmpDir := t.TempDir()
		mockTransfer := testutil.NewMockTransferer()

		syncer := filesync.New(
			filepath.Join(tmpDir, "syncing"),
			filesync.WithTransferer(mockTransfer),
			filesync.WithMaxConcurrent(2),
		)

		dl := createTestDownload("hash1", "TestTorrent", "tv")
		finalPath := filepath.Join(tmpDir, "downloads/tv")
		job := syncer.CreateJob(dl, "test-downloader", finalPath)

		// Create files at staging location
		for _, f := range job.Files {
			require.NoError(t, os.MkdirAll(filepath.Dir(f.LocalPath), 0750))
			require.NoError(t, os.WriteFile(f.LocalPath, make([]byte, f.Size), 0600))
			f.Status = filesync.FileStatusComplete
		}

		err := syncer.MoveToFinal(job)
		require.NoError(t, err)

		// Verify files are at final destination
		for _, f := range job.Files {
			finalFilePath := filepath.Join(finalPath, f.Path)
			info, statErr := os.Stat(finalFilePath)
			require.NoError(t, statErr)
			assert.Equal(t, f.Size, info.Size())
		}

		// Verify staging directory is cleaned up
		_, err = os.Stat(job.LocalBase)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("SkipsIncompleteFiles", func(t *testing.T) {
		tmpDir := t.TempDir()
		syncer := filesync.New(filepath.Join(tmpDir, "syncing"))

		dl := createTestDownload("hash1", "TestTorrent", "tv")
		finalPath := filepath.Join(tmpDir, "downloads/tv")
		job := syncer.CreateJob(dl, "test-downloader", finalPath)

		// Only mark first file as complete
		require.NoError(t, os.MkdirAll(filepath.Dir(job.Files[0].LocalPath), 0750))
		require.NoError(t, os.WriteFile(job.Files[0].LocalPath, make([]byte, job.Files[0].Size), 0600))
		job.Files[0].Status = filesync.FileStatusComplete

		// Second file is still pending (no staging file created)
		job.Files[1].Status = filesync.FileStatusPending

		err := syncer.MoveToFinal(job)
		require.NoError(t, err)

		// First file should be at final destination
		finalFile1 := filepath.Join(finalPath, job.Files[0].Path)
		_, err = os.Stat(finalFile1)
		require.NoError(t, err)

		// Second file should not exist at final destination
		finalFile2 := filepath.Join(finalPath, job.Files[1].Path)
		_, err = os.Stat(finalFile2)
		assert.True(t, os.IsNotExist(err))
	})
}

// --- Close/PrepareShutdown Tests ---

func TestSyncerLifecycle(t *testing.T) {
	t.Run("Close", func(t *testing.T) {
		t.Run("WithTransferer", func(t *testing.T) {
			mockTransfer := testutil.NewMockTransferer()
			syncer := filesync.New("/syncing", filesync.WithTransferer(mockTransfer))

			err := syncer.Close()
			assert.NoError(t, err)
		})

		t.Run("WithoutTransferer", func(t *testing.T) {
			syncer := filesync.New("/syncing")

			err := syncer.Close()
			assert.NoError(t, err)
		})
	})

	t.Run("PrepareShutdown", func(_ *testing.T) {
		mockTransfer := testutil.NewMockTransferer()
		syncer := filesync.New("/syncing", filesync.WithTransferer(mockTransfer))

		// Should not panic
		syncer.PrepareShutdown()
	})
}

// --- Helper Functions ---

func createTestDownload(id, name, category string) *download.Download {
	return &download.Download{
		ID:       id,
		Name:     name,
		Hash:     id,
		Category: category,
		State:    download.TorrentStateComplete,
		SavePath: "/remote/downloads",
		Size:     1024 * 1024,
		Progress: 1.0,
		Files: []download.File{
			{
				Path:       name + "/file1.mkv",
				Size:       512 * 1024,
				Downloaded: 512 * 1024,
				State:      download.FileStateComplete,
				Priority:   1,
			},
			{
				Path:       name + "/file2.mkv",
				Size:       512 * 1024,
				Downloaded: 512 * 1024,
				State:      download.FileStateComplete,
				Priority:   1,
			},
		},
	}
}

// createTestJob creates a SyncJob via the Syncer.CreateJob method to ensure
// proper initialization of unexported fields like ctx and cancel.
func createTestJob() *filesync.SyncJob {
	const (
		name     = "TestTorrent"
		category = "tv"
	)
	syncer := filesync.New("/syncing")
	dl := &download.Download{
		ID:       "test-job-" + name,
		Name:     name,
		Hash:     "test-job-" + name,
		Category: category,
		SavePath: "/remote/downloads",
		Files: []download.File{
			{
				Path:     name + "/file1.mkv",
				Size:     512 * 1024,
				State:    download.FileStateComplete,
				Priority: 1,
			},
			{
				Path:     name + "/file2.mkv",
				Size:     512 * 1024,
				State:    download.FileStateComplete,
				Priority: 1,
			},
		},
	}
	return syncer.CreateJob(dl, "test-downloader", "/downloads/"+category)
}
