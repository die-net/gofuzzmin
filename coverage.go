package main

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// seedResult holds the coverage data for a single seed.
type seedResult struct {
	name    string // original cache filename
	path    string // original cache path
	fileLen int    // seed file size in bytes
	newBits int    // new coverage bits contributed (from fuzzer warmup)
}

// warmupResult holds the parsed output from a fuzzer warmup run.
type warmupResult struct {
	entries          []warmupEntry
	totalEntries     int
	initialCovBits   int
	warmupEntryCount int // from "gathering baseline coverage: 0/N"
}

type warmupEntry struct {
	newBits int
}

var (
	processedRe    = regexp.MustCompile(`new bits: (\d+)`)
	finishedRe     = regexp.MustCompile(`finished processing input corpus, entries: (\d+), initial coverage bits: (\d+)`)
	gatheringRe    = regexp.MustCompile(`gathering baseline coverage: 0/(\d+) completed`)
	testPassFailRe = regexp.MustCompile(`^(ok|FAIL)\s`)
)

// collectCoverage runs the fuzzer warmup for the given corpus through binPath,
// using the fuzzer's own edge-counter coverage. Seeds are sorted shortest-first
// and processed in that order so shorter seeds establish coverage first.
func collectCoverage(ctx context.Context, cfg config, corpus *corpusInfo, binPath string) ([]seedResult, int, error) {
	// Sort seeds by file size (ascending), then name for determinism.
	sorted := make([]seedFile, len(corpus.cacheSeeds))
	copy(sorted, corpus.cacheSeeds)
	slices.SortFunc(sorted, func(a, b seedFile) int {
		if c := cmp.Compare(a.inputLen, b.inputLen); c != 0 {
			return c
		}
		return cmp.Compare(a.name, b.name)
	})

	// Step 1: Get baseline count (f.Add + testdata seeds).
	baselineCount, err := getBaselineCount(ctx, cfg, corpus, binPath)
	if err != nil {
		return nil, 0, fmt.Errorf("getting baseline count: %w", err)
	}
	if cfg.verbose {
		fmt.Printf("  baseline seeds (f.Add + testdata): %d\n", baselineCount)
	}

	// Step 2: Prepare temp cache dir with seeds in sorted order.
	cacheDir, cleanup, err := prepareSortedCache(corpus.funcName, sorted)
	if err != nil {
		return nil, 0, fmt.Errorf("preparing sorted cache: %w", err)
	}
	defer cleanup()

	// Step 3: Run warmup with all seeds.
	result, err := runWarmup(ctx, cfg, corpus, binPath, cacheDir)
	if err != nil {
		return nil, 0, fmt.Errorf("running warmup: %w", err)
	}

	// Step 4: Map warmup entries to seeds.
	cacheEntryCount := max(len(result.entries)-baselineCount, 0)

	if cacheEntryCount != len(sorted) && cfg.verbose {
		fmt.Printf("  warning: expected %d cache entries in warmup, got %d (likely hash duplicates with baseline)\n",
			len(sorted), cacheEntryCount)
	}

	results := make([]seedResult, len(sorted))
	for i, s := range sorted {
		results[i] = seedResult{
			name:    s.name,
			path:    s.path,
			fileLen: s.inputLen,
		}
		warmupIdx := baselineCount + i
		if warmupIdx < len(result.entries) {
			results[i].newBits = result.entries[warmupIdx].newBits
		}
		// Seeds beyond the warmup entries were hash-duplicates; newBits stays 0.
	}

	return results, result.initialCovBits, nil
}

// getBaselineCount runs a warmup with an empty cache to determine how many
// seeds come from f.Add() and testdata (the baseline, before cache seeds).
func getBaselineCount(ctx context.Context, cfg config, corpus *corpusInfo, binPath string) (int, error) {
	emptyDir, err := os.MkdirTemp("", "gofuzzmin-empty-*")
	if err != nil {
		return 0, err
	}
	defer func() { _ = os.RemoveAll(emptyDir) }()

	// Create the function subdirectory (empty) so the binary doesn't error.
	if err := os.MkdirAll(filepath.Join(emptyDir, corpus.funcName), 0o755); err != nil {
		return 0, err
	}

	result, err := runWarmup(ctx, cfg, corpus, binPath, emptyDir)
	if err != nil {
		return 0, err
	}
	return len(result.entries), nil
}

// prepareSortedCache copies seeds to a temp cache directory with sequential
// numeric filenames so os.ReadDir (alphabetical) preserves our sort order.
// The directory structure matches what -test.fuzzcachedir expects:
// <parent>/<FuncName>/<seeds...>
func prepareSortedCache(funcName string, sorted []seedFile) (parentDir string, cleanup func(), err error) {
	parentDir, err = os.MkdirTemp("", "gofuzzmin-cache-*")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { _ = os.RemoveAll(parentDir) }
	defer func() {
		if err != nil {
			cleanup()
			cleanup = nil
		}
	}()

	seedDir := filepath.Join(parentDir, funcName)
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		return "", nil, err
	}

	for i, s := range sorted {
		name := fmt.Sprintf("%06d", i)
		dst := filepath.Join(seedDir, name)
		data, err := os.ReadFile(s.path)
		if err != nil {
			return "", nil, fmt.Errorf("reading seed %s: %w", s.name, err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return "", nil, fmt.Errorf("writing seed: %w", err)
		}
	}
	return parentDir, cleanup, nil
}

// runWarmup starts the test binary in fuzz mode with GODEBUG=fuzzdebug=1,
// parses the warmup debug output, and kills the process once warmup is done.
//
// The binary runs from a temp working directory that symlinks testdata
// (excluding fuzz/<FuncName>/) so that testdata regression seeds don't
// participate in warmup — they can fail and aren't what we're minimizing.
func runWarmup(ctx context.Context, cfg config, corpus *corpusInfo, binPath, cacheDir string) (*warmupResult, error) {
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	workDir, err := setupWorkDir(corpus)
	if err != nil {
		return nil, fmt.Errorf("setting up work dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	cmd := exec.CommandContext(ctx, binPath, //nolint:gosec // binPath is a binary we just built
		"-test.fuzz=^"+corpus.funcName+"$",
		"-test.fuzzcachedir="+cacheDir,
		"-test.parallel=1",
	)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "GODEBUG=fuzzdebug=1")
	cmd.Stdin = nil
	cmd.Stdout = nil

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("piping stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting test binary: %w", err)
	}

	result, parseErr := parseWarmupOutput(stderr)

	// Kill the process — we only needed the warmup phase.
	_ = cmd.Process.Kill()
	_ = cmd.Wait()

	if parseErr != nil {
		return nil, parseErr
	}
	return result, nil
}

// setupWorkDir creates a temp working directory that mirrors the package's
// testdata layout via symlinks, excluding testdata/fuzz/<FuncName>/ so that
// testdata regression seeds (which may fail) are excluded from warmup.
func setupWorkDir(corpus *corpusInfo) (workDir string, err error) {
	wd, err := os.MkdirTemp("", "gofuzzmin-wd-*")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(wd)
		}
	}()

	srcTestdata := filepath.Join(corpus.pkgDir, "testdata")
	dstTestdata := filepath.Join(wd, "testdata")

	if err := os.MkdirAll(dstTestdata, 0o755); err != nil {
		return "", err
	}

	entries, err := os.ReadDir(srcTestdata)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return wd, nil
		}
		return "", err
	}

	for _, e := range entries {
		if e.Name() == "fuzz" {
			if err := symlinkFuzzDirExcluding(corpus, dstTestdata); err != nil {
				return "", err
			}
			continue
		}
		src := filepath.Join(srcTestdata, e.Name())
		dst := filepath.Join(dstTestdata, e.Name())
		if err := os.Symlink(src, dst); err != nil {
			return "", err
		}
	}

	return wd, nil
}

// symlinkFuzzDirExcluding symlinks all fuzz subdirectories except the one
// for corpus.funcName, so other fuzz targets' testdata remains accessible.
func symlinkFuzzDirExcluding(corpus *corpusInfo, dstTestdata string) error {
	srcFuzz := filepath.Join(corpus.pkgDir, "testdata", "fuzz")
	dstFuzz := filepath.Join(dstTestdata, "fuzz")
	if err := os.MkdirAll(dstFuzz, 0o755); err != nil {
		return err
	}

	entries, err := os.ReadDir(srcFuzz)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}

	for _, e := range entries {
		if e.Name() == corpus.funcName {
			if err := os.MkdirAll(filepath.Join(dstFuzz, e.Name()), 0o755); err != nil {
				return err
			}
			continue
		}
		src := filepath.Join(srcFuzz, e.Name())
		dst := filepath.Join(dstFuzz, e.Name())
		if err := os.Symlink(src, dst); err != nil {
			return err
		}
	}
	return nil
}

// parseWarmupOutput reads fuzzer debug output line by line and returns when
// the warmup phase completes (signaled by the "finished processing" line).
func parseWarmupOutput(r io.Reader) (*warmupResult, error) {
	var result warmupResult
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Text()

		if m := gatheringRe.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			result.warmupEntryCount = n
			continue
		}

		if m := processedRe.FindStringSubmatch(line); m != nil && strings.Contains(line, "processed an initial input") {
			bits, _ := strconv.Atoi(m[1])
			result.entries = append(result.entries, warmupEntry{newBits: bits})
			continue
		}

		if m := finishedRe.FindStringSubmatch(line); m != nil {
			result.totalEntries, _ = strconv.Atoi(m[1])
			result.initialCovBits, _ = strconv.Atoi(m[2])
			return &result, nil
		}

		// If the test exits (PASS/FAIL) before we see the finished line,
		// warmup was too short or the target crashed.
		if testPassFailRe.MatchString(line) {
			return &result, fmt.Errorf("test exited before warmup completed: %s", line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading warmup output: %w", err)
	}
	return nil, errors.New("warmup output ended unexpectedly (no 'finished processing' line)")
}
