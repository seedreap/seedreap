package testing

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/crypto/ssh"
)

// SSH container configuration constants.
const (
	sshContainerStartupTimeout = 60 * time.Second
	sshConnectionTimeout       = 5 * time.Second
	sshRetryInterval           = 500 * time.Millisecond
	sshKeyBits                 = 4096
	blockSizeBytes             = 1024 * 1024 // 1 MB
)

// SSHContainer holds references to a running SSH container for integration tests.
type SSHContainer struct {
	Container     testcontainers.Container
	Host          string
	Port          int
	User          string
	PrivateKey    string // Path to private key file
	PublicKey     string // Public key content
	RemoteDir     string // Directory for test files on remote
	KeysDir       string // Temporary directory for keys
	IgnoreHostKey bool   // Whether to ignore host key verification
}

// SSHContainerConfig configures the SSH container.
type SSHContainerConfig struct {
	// User is the SSH username (default: "testuser")
	User string
	// RemoteDir is the directory for test files (default: "/data")
	RemoteDir string
}

// DefaultSSHContainerConfig returns the default configuration.
func DefaultSSHContainerConfig() SSHContainerConfig {
	return SSHContainerConfig{
		User:      "testuser",
		RemoteDir: "/data",
	}
}

// StartSSHContainer starts an SSH container for integration testing.
// The container uses linuxserver/openssh-server which provides a simple SSH server.
// Returns an SSHContainer with connection details and cleans up on context cancellation.
func StartSSHContainer(ctx context.Context, cfg SSHContainerConfig) (*SSHContainer, error) {
	if cfg.User == "" {
		cfg.User = "testuser"
	}
	if cfg.RemoteDir == "" {
		cfg.RemoteDir = "/data"
	}

	// Generate SSH key pair
	keysDir, privateKeyPath, publicKey, err := generateSSHKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate SSH key pair: %w", err)
	}

	// Create the container request
	// Using linuxserver/openssh-server as it's well-maintained and easy to configure
	req := testcontainers.ContainerRequest{
		Image:        "linuxserver/openssh-server:latest",
		ExposedPorts: []string{"2222/tcp"},
		Env: map[string]string{
			"PUID":            "1000",
			"PGID":            "1000",
			"TZ":              "UTC",
			"USER_NAME":       cfg.User,
			"PUBLIC_KEY":      publicKey,
			"SUDO_ACCESS":     "false",
			"PASSWORD_ACCESS": "false",
		},
		WaitingFor: wait.ForLog("sshd is listening on port").WithStartupTimeout(sshContainerStartupTimeout),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		_ = os.RemoveAll(keysDir)
		return nil, fmt.Errorf("failed to start SSH container: %w", err)
	}

	// Get the mapped port
	mappedPort, err := container.MappedPort(ctx, "2222")
	if err != nil {
		_ = container.Terminate(ctx)
		_ = os.RemoveAll(keysDir)
		return nil, fmt.Errorf("failed to get mapped port: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		_ = os.RemoveAll(keysDir)
		return nil, fmt.Errorf("failed to get container host: %w", err)
	}

	// Create test data directory in container
	exitCode, _, err := container.Exec(ctx, []string{"mkdir", "-p", cfg.RemoteDir})
	if err != nil || exitCode != 0 {
		_ = container.Terminate(ctx)
		_ = os.RemoveAll(keysDir)
		return nil, fmt.Errorf("failed to create remote directory: %w (exit code: %d)", err, exitCode)
	}

	// Set permissions on the remote directory
	exitCode, _, err = container.Exec(ctx, []string{"chown", "-R", cfg.User + ":" + cfg.User, cfg.RemoteDir})
	if err != nil || exitCode != 0 {
		_ = container.Terminate(ctx)
		_ = os.RemoveAll(keysDir)
		return nil, fmt.Errorf("failed to set permissions on remote directory: %w (exit code: %d)", err, exitCode)
	}

	return &SSHContainer{
		Container:     container,
		Host:          host,
		Port:          mappedPort.Int(),
		User:          cfg.User,
		PrivateKey:    privateKeyPath,
		PublicKey:     publicKey,
		RemoteDir:     cfg.RemoteDir,
		KeysDir:       keysDir,
		IgnoreHostKey: true, // For testing, we ignore host key verification
	}, nil
}

// Cleanup stops the container and removes temporary files.
func (s *SSHContainer) Cleanup(ctx context.Context) error {
	var errs []error

	if s.Container != nil {
		if err := s.Container.Terminate(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to terminate container: %w", err))
		}
	}

	if s.KeysDir != "" {
		if err := os.RemoveAll(s.KeysDir); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove keys directory: %w", err))
		}
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// CreateTestFile creates a test file in the container with the given content.
func (s *SSHContainer) CreateTestFile(ctx context.Context, relativePath string, content []byte) error {
	fullPath := filepath.Join(s.RemoteDir, relativePath)
	dir := filepath.Dir(fullPath)

	// Create parent directory
	exitCode, _, err := s.Container.Exec(ctx, []string{"mkdir", "-p", dir})
	if err != nil || exitCode != 0 {
		return fmt.Errorf("failed to create directory %s: %w (exit code: %d)", dir, err, exitCode)
	}

	// Write content using echo and base64 to handle binary data
	// For simplicity, we'll use dd with /dev/zero for binary test files
	// or echo for text content
	if len(content) == 0 {
		exitCode, _, err = s.Container.Exec(ctx, []string{"touch", fullPath})
	} else {
		// Write content to a temporary file and copy it
		// Using printf to handle binary content
		exitCode, _, err = s.Container.Exec(ctx, []string{
			"sh", "-c",
			fmt.Sprintf("printf '%%s' '%s' > %s", string(content), fullPath),
		})
	}
	if err != nil || exitCode != 0 {
		return fmt.Errorf("failed to create file %s: %w (exit code: %d)", fullPath, err, exitCode)
	}

	// Set ownership
	exitCode, _, err = s.Container.Exec(ctx, []string{"chown", s.User + ":" + s.User, fullPath})
	if err != nil || exitCode != 0 {
		return fmt.Errorf("failed to set ownership on %s: %w (exit code: %d)", fullPath, err, exitCode)
	}

	return nil
}

// CreateTestFileWithSize creates a test file of the specified size using dd.
func (s *SSHContainer) CreateTestFileWithSize(ctx context.Context, relativePath string, sizeBytes int64) error {
	fullPath := filepath.Join(s.RemoteDir, relativePath)
	dir := filepath.Dir(fullPath)

	// Create parent directory
	exitCode, _, err := s.Container.Exec(ctx, []string{"mkdir", "-p", dir})
	if err != nil || exitCode != 0 {
		return fmt.Errorf("failed to create directory %s: %w (exit code: %d)", dir, err, exitCode)
	}

	// Use dd to create file with random data
	// Using urandom for variety in the content
	exitCode, _, err = s.Container.Exec(ctx, []string{
		"dd",
		"if=/dev/urandom",
		fmt.Sprintf("of=%s", fullPath),
		fmt.Sprintf("bs=%d", min(sizeBytes, blockSizeBytes)),
		fmt.Sprintf("count=%d", (sizeBytes+blockSizeBytes-1)/blockSizeBytes),
	})
	if err != nil || exitCode != 0 {
		return fmt.Errorf("failed to create file %s with dd: %w (exit code: %d)", fullPath, err, exitCode)
	}

	// Truncate to exact size if needed
	if sizeBytes > 0 {
		exitCode, _, err = s.Container.Exec(ctx, []string{
			"truncate", "-s", strconv.FormatInt(sizeBytes, 10), fullPath,
		})
		if err != nil || exitCode != 0 {
			return fmt.Errorf("failed to truncate file %s: %w (exit code: %d)", fullPath, err, exitCode)
		}
	}

	// Set ownership
	exitCode, _, err = s.Container.Exec(ctx, []string{"chown", s.User + ":" + s.User, fullPath})
	if err != nil || exitCode != 0 {
		return fmt.Errorf("failed to set ownership on %s: %w (exit code: %d)", fullPath, err, exitCode)
	}

	return nil
}

// WaitForSSH waits for the SSH server to be ready to accept connections.
func (s *SSHContainer) WaitForSSH(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	// Read private key
	keyData, err := os.ReadFile(s.PrivateKey)
	if err != nil {
		return fmt.Errorf("failed to read private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: s.User,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // test only
		Timeout:         sshConnectionTimeout,
	}

	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)

	for time.Now().Before(deadline) {
		client, dialErr := ssh.Dial("tcp", addr, config)
		if dialErr == nil {
			_ = client.Close()
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sshRetryInterval):
			// Retry
		}
	}

	return fmt.Errorf("timeout waiting for SSH server at %s", addr)
}

// generateSSHKeyPair generates an RSA key pair and returns paths to the files.
//
//nolint:nonamedreturns // named returns document the multiple string return values
func generateSSHKeyPair() (keysDir, privateKeyPath, publicKey string, err error) {
	// Create temporary directory for keys
	keysDir, err = os.MkdirTemp("", "ssh-test-keys-")
	if err != nil {
		return "", "", "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Generate RSA key
	privateRSAKey, err := rsa.GenerateKey(rand.Reader, sshKeyBits)
	if err != nil {
		_ = os.RemoveAll(keysDir)
		return "", "", "", fmt.Errorf("failed to generate RSA key: %w", err)
	}

	// Encode private key to PEM
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateRSAKey),
	})

	// Write private key
	privateKeyPath = filepath.Join(keysDir, "id_rsa")
	if writeErr := os.WriteFile(privateKeyPath, privateKeyPEM, 0600); writeErr != nil {
		_ = os.RemoveAll(keysDir)
		return "", "", "", fmt.Errorf("failed to write private key: %w", writeErr)
	}

	// Generate public key in OpenSSH format
	sshPubKey, err := ssh.NewPublicKey(&privateRSAKey.PublicKey)
	if err != nil {
		_ = os.RemoveAll(keysDir)
		return "", "", "", fmt.Errorf("failed to create SSH public key: %w", err)
	}

	publicKey = string(ssh.MarshalAuthorizedKey(sshPubKey))

	return keysDir, privateKeyPath, publicKey, nil
}
