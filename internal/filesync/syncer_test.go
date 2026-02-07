package filesync_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seedreap/seedreap/internal/filesync"
	testutil "github.com/seedreap/seedreap/internal/testing"
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
