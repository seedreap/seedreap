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

func TestSafeJoin(t *testing.T) {
	t.Run("ValidPaths", func(t *testing.T) {
		tests := []struct {
			name     string
			base     string
			path     string
			expected string
		}{
			{
				name:     "simple file",
				base:     "/base",
				path:     "file.txt",
				expected: "/base/file.txt",
			},
			{
				name:     "nested path",
				base:     "/base",
				path:     "subdir/file.txt",
				expected: "/base/subdir/file.txt",
			},
			{
				name:     "deep nested path",
				base:     "/base",
				path:     "a/b/c/d/file.txt",
				expected: "/base/a/b/c/d/file.txt",
			},
			{
				name:     "torrent-style path",
				base:     "/downloads",
				path:     "TorrentName/Season 01/episode.mkv",
				expected: "/downloads/TorrentName/Season 01/episode.mkv",
			},
			{
				name:     "path with dots in filename",
				base:     "/base",
				path:     "file.name.with.dots.txt",
				expected: "/base/file.name.with.dots.txt",
			},
			{
				name:     "single dot current dir",
				base:     "/base",
				path:     "./file.txt",
				expected: "/base/file.txt",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result, err := fileutil.SafeJoin(tt.base, tt.path)
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("PathTraversalAttacks", func(t *testing.T) {
		base := "/base/dir"

		tests := []struct {
			name string
			path string
		}{
			{
				name: "simple parent traversal",
				path: "../etc/passwd",
			},
			{
				name: "double parent traversal",
				path: "../../etc/passwd",
			},
			{
				name: "traversal with subdir prefix",
				path: "subdir/../../etc/passwd",
			},
			{
				name: "multiple traversals",
				path: "../../../../../../../etc/passwd",
			},
			{
				name: "traversal at start",
				path: "../sibling/file.txt",
			},
			{
				name: "hidden traversal with dot segments",
				path: "foo/../../../bar",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := fileutil.SafeJoin(base, tt.path)
				require.Error(t, err)
				assert.Contains(t, err.Error(), "path")
			})
		}
	})

	t.Run("AbsolutePaths", func(t *testing.T) {
		base := "/base"

		tests := []struct {
			name string
			path string
		}{
			{
				name: "absolute unix path",
				path: "/etc/passwd",
			},
			{
				name: "absolute with traversal",
				path: "/base/../etc/passwd",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := fileutil.SafeJoin(base, tt.path)
				require.Error(t, err)
				assert.Contains(t, err.Error(), "relative")
			})
		}
	})

	t.Run("RealFilesystem", func(t *testing.T) {
		tmpDir := t.TempDir()

		t.Run("valid path works", func(t *testing.T) {
			result, err := fileutil.SafeJoin(tmpDir, "subdir/file.txt")
			require.NoError(t, err)
			assert.Equal(t, filepath.Join(tmpDir, "subdir/file.txt"), result)
		})

		t.Run("traversal blocked", func(t *testing.T) {
			_, err := fileutil.SafeJoin(tmpDir, "../outside.txt")
			require.Error(t, err)
		})
	})
}
