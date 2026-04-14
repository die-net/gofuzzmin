package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseWarmupOutput(t *testing.T) {
	t.Run("basic warmup", func(t *testing.T) {
		output := `fuzz: elapsed: 0s, gathering baseline coverage: 0/5 completed
2026-04-14 05:16:44.226749 DEBUG processed an initial input, id: , new bits: 4, size: 0, exec time: 63.917µs
2026-04-14 05:16:44.22686 DEBUG processed an initial input, id: , new bits: 2, size: 0, exec time: 92.292µs
2026-04-14 05:16:44.227586 DEBUG processed an initial input, id: , new bits: 0, size: 0, exec time: 14.125µs
2026-04-14 05:16:44.228129 DEBUG processed an initial input, id: , new bits: 1, size: 0, exec time: 14.041µs
2026-04-14 05:16:44.229125 DEBUG processed an initial input, id: , new bits: 0, size: 0, exec time: 48.917µs
fuzz: elapsed: 0s, gathering baseline coverage: 5/5 completed, now fuzzing with 1 workers
2026-04-14 05:16:44.230322 DEBUG finished processing input corpus, entries: 5, initial coverage bits: 7
`
		result, err := parseWarmupOutput(strings.NewReader(output))
		if err != nil {
			t.Fatal(err)
		}

		if result.warmupEntryCount != 5 {
			t.Errorf("warmupEntryCount = %d, want 5", result.warmupEntryCount)
		}
		if result.totalEntries != 5 {
			t.Errorf("totalEntries = %d, want 5", result.totalEntries)
		}
		if result.initialCovBits != 7 {
			t.Errorf("initialCovBits = %d, want 7", result.initialCovBits)
		}
		if len(result.entries) != 5 {
			t.Fatalf("len(entries) = %d, want 5", len(result.entries))
		}

		wantBits := []int{4, 2, 0, 1, 0}
		for i, want := range wantBits {
			if result.entries[i].newBits != want {
				t.Errorf("entry[%d].newBits = %d, want %d", i, result.entries[i].newBits, want)
			}
		}
	})

	t.Run("empty corpus", func(t *testing.T) {
		output := `fuzz: elapsed: 0s, gathering baseline coverage: 0/0 completed, now fuzzing with 1 workers
2026-04-14 05:16:44.230322 DEBUG finished processing input corpus, entries: 0, initial coverage bits: 0
`
		result, err := parseWarmupOutput(strings.NewReader(output))
		if err != nil {
			t.Fatal(err)
		}
		if len(result.entries) != 0 {
			t.Errorf("len(entries) = %d, want 0", len(result.entries))
		}
	})

	t.Run("unexpected exit", func(t *testing.T) {
		output := `fuzz: elapsed: 0s, gathering baseline coverage: 0/5 completed
ok  	testpkg	0.1s
`
		_, err := parseWarmupOutput(strings.NewReader(output))
		if err == nil {
			t.Fatal("expected error for early exit")
		}
		if !strings.Contains(err.Error(), "test exited before warmup completed") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("unexpected EOF", func(t *testing.T) {
		output := `fuzz: elapsed: 0s, gathering baseline coverage: 0/5 completed
2026-04-14 05:16:44.226749 DEBUG processed an initial input, id: , new bits: 4, size: 0, exec time: 63.917µs
`
		_, err := parseWarmupOutput(strings.NewReader(output))
		if err == nil {
			t.Fatal("expected error for truncated output")
		}
		if !strings.Contains(err.Error(), "no 'finished processing' line") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("FAIL exit", func(t *testing.T) {
		output := `fuzz: elapsed: 0s, gathering baseline coverage: 0/5 completed
FAIL	testpkg	0.1s
`
		_, err := parseWarmupOutput(strings.NewReader(output))
		if err == nil {
			t.Fatal("expected error for FAIL exit")
		}
		if !strings.Contains(err.Error(), "test exited before warmup completed") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestParseFuzzFileLen(t *testing.T) {
	dir := t.TempDir()

	t.Run("string seed", func(t *testing.T) {
		p := filepath.Join(dir, "seed1")
		writeFile(t, p, "go test fuzz v1\nstring(\"hello\")\n")
		got := parseFuzzFileLen(p)
		want := len("string(\"hello\")\n")
		if got != want {
			t.Errorf("parseFuzzFileLen = %d, want %d", got, want)
		}
	})

	t.Run("no header", func(t *testing.T) {
		p := filepath.Join(dir, "seed2")
		writeFile(t, p, "no header here")
		got := parseFuzzFileLen(p)
		if got != len("no header here") {
			t.Errorf("parseFuzzFileLen = %d, want %d", got, len("no header here"))
		}
	})

	t.Run("missing file", func(t *testing.T) {
		got := parseFuzzFileLen(filepath.Join(dir, "nonexistent"))
		if got != 0 {
			t.Errorf("parseFuzzFileLen for missing file = %d, want 0", got)
		}
	})
}
