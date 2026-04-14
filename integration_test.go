package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestIntegration uses a trivial fuzz target to exercise the full pipeline:
// corpus discovery, binary build, warmup-based coverage, and minimization.
func TestIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pkgDir, err := filepath.Abs("testdata/integration")
	if err != nil {
		t.Fatal(err)
	}

	// Populate the fuzz cache with some seeds by running the fuzzer briefly.
	fuzzCmd := exec.CommandContext(ctx, "go", "test", "-fuzz=FuzzTrivial", "-fuzztime=3s", "./...")
	fuzzCmd.Dir = pkgDir
	fuzzCmd.Stdout = os.Stderr
	fuzzCmd.Stderr = os.Stderr
	_ = fuzzCmd.Run()

	// Add several redundant seeds manually to the cache.
	importPath := "testintegration"
	cacheDir, err := fuzzCacheDir(ctx, importPath, "FuzzTrivial")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}

	redundantSeeds := []string{
		"go test fuzz v1\nstring(\"aaa\")\n",
		"go test fuzz v1\nstring(\"aab\")\n",
		"go test fuzz v1\nstring(\"aac\")\n",
		"go test fuzz v1\nstring(\"xhello\")\n",
		"go test fuzz v1\nstring(\"xyworld\")\n",
	}
	for i, content := range redundantSeeds {
		name := strings.Repeat("f", 16) + string(rune('0'+i))
		p := filepath.Join(cacheDir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Count seeds before minimization.
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	beforeCount := len(entries)
	t.Logf("cache seeds before: %d", beforeCount)
	if beforeCount < 3 {
		t.Skip("not enough cache seeds to test minimization")
	}

	// Discover corpus.
	corpus, err := discoverCorpus(ctx, pkgDir, "FuzzTrivial")
	if err != nil {
		t.Fatal(err)
	}
	if len(corpus.cacheSeeds) != beforeCount {
		t.Fatalf("discovered %d seeds, expected %d", len(corpus.cacheSeeds), beforeCount)
	}

	// Build test binary.
	binPath, cleanup, err := buildTestBinary(ctx, pkgDir)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	// Collect coverage.
	cfg := config{
		pkg:     pkgDir,
		timeout: 30 * time.Second,
		verbose: true,
	}
	results, totalCovBits, err := collectCoverage(ctx, cfg, corpus, binPath)
	if err != nil {
		t.Fatal(err)
	}

	// Verify we got some coverage data.
	var withCoverage int
	for _, r := range results {
		if r.newBits > 0 {
			withCoverage++
		}
	}
	t.Logf("seeds with new bits: %d/%d, total coverage bits: %d",
		withCoverage, len(results), totalCovBits)
	if totalCovBits == 0 {
		t.Fatal("no coverage bits found")
	}

	// Minimize.
	selected := minimize(results)
	t.Logf("selected: %d, removable: %d",
		len(selected), len(results)-len(selected))

	if len(selected) == 0 {
		t.Fatal("minimize returned empty selection")
	}
	if len(selected) >= len(results) {
		t.Error("minimize didn't reduce the corpus at all")
	}
}
