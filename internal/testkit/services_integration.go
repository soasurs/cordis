//go:build integration

package testkit

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// BuildService compiles the main package at pkgPath and returns the path of
// the built binary.
func BuildService(t *testing.T, pkgPath string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "service")
	cmd := exec.Command("go", "build", "-o", binary, pkgPath)
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "build %s:\n%s", pkgPath, output)
	return binary
}

// StartService writes configYAML to a temporary file and starts the service
// binary with it. The process is terminated during test cleanup.
func StartService(t *testing.T, binary, configYAML string) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(configYAML), 0o600))

	cmd := exec.Command(binary, "-c", configPath)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Start())

	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})
}

// FreeAddress returns a 127.0.0.1 address with an ephemeral free port.
func FreeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	return address
}

// WaitServiceReady polls probe until it returns nil or the timeout expires.
func WaitServiceReady(t *testing.T, timeout time.Duration, probe func(ctx context.Context) error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		last = probe(ctx)
		cancel()
		if last == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("service not ready within %s: %v", timeout, last)
}
