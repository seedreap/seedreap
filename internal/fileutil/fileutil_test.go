package fileutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seedreap/seedreap/internal/fileutil"
)

func TestCopyFile(t *testing.T) {
	t.Run("SuccessCases", func(t *testing.T) {
		tests := []struct {
			name    string
			content []byte
		}{
			{
				name:    "copies small file",
				content: []byte("hello world"),
			},
			{
				name:    "copies empty file",
				content: []byte{},
			},
			{
				name:    "copies binary content",
				content: []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD},
			},
			{
				name:    "copies large file",
				content: make([]byte, 1024*1024), // 1MB
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tmpDir := t.TempDir()

				srcPath := filepath.Join(tmpDir, "source.txt")
				dstPath := filepath.Join(tmpDir, "dest.txt")

				// Create source file
				err := os.WriteFile(srcPath, tt.content, 0600)
				require.NoError(t, err)

				// Copy file
				err = fileutil.CopyFile(srcPath, dstPath)
				require.NoError(t, err)

				// Verify destination exists
				dstContent, err := os.ReadFile(dstPath)
				require.NoError(t, err)

				// Verify content matches
				assert.Equal(t, tt.content, dstContent)

				// Verify source still exists
				srcContent, err := os.ReadFile(srcPath)
				require.NoError(t, err)
				assert.Equal(t, tt.content, srcContent)
			})
		}
	})

	t.Run("CreatesParentDirectories", func(t *testing.T) {
		tmpDir := t.TempDir()

		srcPath := filepath.Join(tmpDir, "source.txt")
		dstPath := filepath.Join(tmpDir, "deep", "nested", "dir", "dest.txt")

		content := []byte("test content")

		// Create source file
		err := os.WriteFile(srcPath, content, 0600)
		require.NoError(t, err)

		// Copy file - should create intermediate directories
		err = fileutil.CopyFile(srcPath, dstPath)
		require.NoError(t, err)

		// Verify destination exists with correct content
		dstContent, err := os.ReadFile(dstPath)
		require.NoError(t, err)
		assert.Equal(t, content, dstContent)
	})

	t.Run("OverwritesExistingFile", func(t *testing.T) {
		tmpDir := t.TempDir()

		srcPath := filepath.Join(tmpDir, "source.txt")
		dstPath := filepath.Join(tmpDir, "dest.txt")

		srcContent := []byte("new content")
		oldContent := []byte("old content that should be replaced")

		// Create source and existing destination
		err := os.WriteFile(srcPath, srcContent, 0600)
		require.NoError(t, err)
		err = os.WriteFile(dstPath, oldContent, 0600)
		require.NoError(t, err)

		// Copy file
		err = fileutil.CopyFile(srcPath, dstPath)
		require.NoError(t, err)

		// Verify destination has new content
		dstContent, err := os.ReadFile(dstPath)
		require.NoError(t, err)
		assert.Equal(t, srcContent, dstContent)
	})

	t.Run("ErrorCases", func(t *testing.T) {
		t.Run("SourceDoesNotExist", func(t *testing.T) {
			tmpDir := t.TempDir()

			srcPath := filepath.Join(tmpDir, "nonexistent.txt")
			dstPath := filepath.Join(tmpDir, "dest.txt")

			err := fileutil.CopyFile(srcPath, dstPath)
			require.Error(t, err)
			assert.True(t, os.IsNotExist(err))
		})

		t.Run("DestinationDirectoryNotWritable", func(t *testing.T) {
			tmpDir := t.TempDir()

			srcPath := filepath.Join(tmpDir, "source.txt")
			readOnlyDir := filepath.Join(tmpDir, "readonly")
			dstPath := filepath.Join(readOnlyDir, "dest.txt")

			// Create source file
			err := os.WriteFile(srcPath, []byte("content"), 0600)
			require.NoError(t, err)

			// Create read-only directory
			err = os.MkdirAll(readOnlyDir, 0500)
			require.NoError(t, err)

			// Ensure we restore permissions for cleanup
			t.Cleanup(func() {
				_ = os.Chmod(readOnlyDir, 0700)
			})

			// Copy should fail
			err = fileutil.CopyFile(srcPath, dstPath)
			require.Error(t, err)
		})

		t.Run("SourceIsDirectory", func(t *testing.T) {
			tmpDir := t.TempDir()

			srcPath := filepath.Join(tmpDir, "srcdir")
			dstPath := filepath.Join(tmpDir, "dest.txt")

			// Create source as directory
			err := os.MkdirAll(srcPath, 0750)
			require.NoError(t, err)

			// Copy should fail
			err = fileutil.CopyFile(srcPath, dstPath)
			require.Error(t, err)
		})
	})

	t.Run("PreservesContent", func(t *testing.T) {
		t.Run("WithNewlines", func(t *testing.T) {
			tmpDir := t.TempDir()

			srcPath := filepath.Join(tmpDir, "source.txt")
			dstPath := filepath.Join(tmpDir, "dest.txt")

			content := []byte("line1\nline2\r\nline3\rline4")

			err := os.WriteFile(srcPath, content, 0600)
			require.NoError(t, err)

			err = fileutil.CopyFile(srcPath, dstPath)
			require.NoError(t, err)

			dstContent, err := os.ReadFile(dstPath)
			require.NoError(t, err)
			assert.Equal(t, content, dstContent)
		})

		t.Run("WithUnicodeContent", func(t *testing.T) {
			tmpDir := t.TempDir()

			srcPath := filepath.Join(tmpDir, "source.txt")
			dstPath := filepath.Join(tmpDir, "dest.txt")

			content := []byte("Hello 世界 🌍 مرحبا")

			err := os.WriteFile(srcPath, content, 0600)
			require.NoError(t, err)

			err = fileutil.CopyFile(srcPath, dstPath)
			require.NoError(t, err)

			dstContent, err := os.ReadFile(dstPath)
			require.NoError(t, err)
			assert.Equal(t, content, dstContent)
		})
	})
}
