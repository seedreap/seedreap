// Package fileutil provides common file operation utilities.
package fileutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SafeJoin safely joins a base path with an untrusted relative path.
// It prevents path traversal attacks by ensuring the result stays within the base directory.
// Returns an error if the path attempts to escape the base directory.
func SafeJoin(base, untrustedPath string) (string, error) {
	// Clean the untrusted path first
	cleaned := filepath.Clean(untrustedPath)

	// Reject absolute paths
	if filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("path must be relative: %s", untrustedPath)
	}

	// Reject paths that start with ..
	if strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("path cannot traverse above base directory: %s", untrustedPath)
	}

	// Join and get the absolute path
	joined := filepath.Join(base, cleaned)
	absJoined, err := filepath.Abs(joined)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}

	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("failed to resolve base path: %w", err)
	}

	// Ensure the joined path is within the base directory
	// Add trailing separator to base to prevent prefix matching issues
	// (e.g., /foo/bar shouldn't match /foo/barbaz)
	if !strings.HasPrefix(absJoined, absBase+string(filepath.Separator)) && absJoined != absBase {
		return "", fmt.Errorf("path escapes base directory: %s", untrustedPath)
	}

	return joined, nil
}

// CopyFile copies a file from src to dst, creating parent directories as needed.
func CopyFile(src, dst string) (retErr error) {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := srcFile.Close(); closeErr != nil && retErr == nil {
			retErr = closeErr
		}
	}()

	if err = os.MkdirAll(filepath.Dir(dst), 0750); err != nil {
		return err
	}

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := dstFile.Close(); closeErr != nil && retErr == nil {
			retErr = closeErr
		}
	}()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// CopyDir recursively copies a directory from src to dst.
func CopyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err = os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err = CopyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err = CopyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}
