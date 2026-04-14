package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// buildTestBinary compiles the test binary for pkg with the fuzzer's
// edge-counter instrumentation (-d=libfuzzer). This is the same
// instrumentation that `go test -fuzz` uses internally.
func buildTestBinary(ctx context.Context, pkg string) (binPath string, cleanup func(), err error) {
	tmpDir, err := os.MkdirTemp("", "gofuzzmin-bin-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(tmpDir) }

	binPath = filepath.Join(tmpDir, "fuzz.test")
	arg := pkg
	if filepath.IsAbs(pkg) {
		arg = "."
	}
	cmd := exec.CommandContext(ctx, "go", "test", //nolint:gosec // args are not user-controlled
		"-c",
		"-gcflags=-d=libfuzzer",
		"-o", binPath,
		arg,
	)
	if filepath.IsAbs(pkg) {
		cmd.Dir = pkg
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("building test binary: %w", err)
	}
	return binPath, cleanup, nil
}
