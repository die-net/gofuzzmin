package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectFuzzFunc(t *testing.T) {
	t.Run("single fuzz function", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "fuzz_test.go"),
			"package p\n\nimport \"testing\"\n\nfunc FuzzFoo(f *testing.F) { f.Fuzz(func(t *testing.T, s string) {}) }\n")

		got, err := detectFuzzFunc(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got != "FuzzFoo" {
			t.Errorf("detectFuzzFunc = %q, want FuzzFoo", got)
		}
	})

	t.Run("multiple fuzz functions", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "fuzz_test.go"),
			"package p\n\nimport \"testing\"\n\nfunc FuzzFoo(f *testing.F) {}\nfunc FuzzBar(f *testing.F) {}\n")

		_, err := detectFuzzFunc(dir)
		if err == nil {
			t.Fatal("expected error for multiple fuzz functions")
		}
		if !strings.Contains(err.Error(), "multiple fuzz functions") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("no fuzz functions", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "foo_test.go"),
			"package p\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) {}\n")

		_, err := detectFuzzFunc(dir)
		if err == nil {
			t.Fatal("expected error for no fuzz functions")
		}
		if !strings.Contains(err.Error(), "no fuzz functions") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("across multiple files", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "a_test.go"),
			"package p\n\nimport \"testing\"\n\nfunc FuzzAlpha(f *testing.F) {}\n")
		writeFile(t, filepath.Join(dir, "b_test.go"),
			"package p\n\nimport \"testing\"\n\nfunc TestBeta(t *testing.T) {}\n")

		got, err := detectFuzzFunc(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got != "FuzzAlpha" {
			t.Errorf("detectFuzzFunc = %q, want FuzzAlpha", got)
		}
	})

	t.Run("non-test files ignored", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "fuzz.go"),
			"package p\n\nfunc FuzzNotATest() {}\n")

		_, err := detectFuzzFunc(dir)
		if err == nil {
			t.Fatal("expected error; non-test file should be ignored")
		}
	})

	t.Run("nonexistent directory", func(t *testing.T) {
		_, err := detectFuzzFunc("/nonexistent/path")
		if err == nil {
			t.Fatal("expected error for nonexistent directory")
		}
	})
}

func TestEnumerateSeeds(t *testing.T) {
	t.Run("basic seeds", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "abc123"), "go test fuzz v1\nstring(\"hello\")\n")
		writeFile(t, filepath.Join(dir, "def456"), "go test fuzz v1\nstring(\"x\")\n")

		seeds, err := enumerateSeeds(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(seeds) != 2 {
			t.Fatalf("len(seeds) = %d, want 2", len(seeds))
		}
		for _, s := range seeds {
			if s.name == "" || s.path == "" {
				t.Errorf("seed has empty name or path: %+v", s)
			}
			if s.inputLen == 0 {
				t.Errorf("seed %s has zero inputLen", s.name)
			}
		}
	})

	t.Run("skips directories", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "seed1"), "go test fuzz v1\nstring(\"a\")\n")
		if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
			t.Fatal(err)
		}

		seeds, err := enumerateSeeds(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(seeds) != 1 {
			t.Fatalf("len(seeds) = %d, want 1", len(seeds))
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		dir := t.TempDir()
		seeds, err := enumerateSeeds(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(seeds) != 0 {
			t.Errorf("len(seeds) = %d, want 0", len(seeds))
		}
	})

	t.Run("nonexistent directory", func(t *testing.T) {
		seeds, err := enumerateSeeds("/nonexistent/path")
		if err != nil {
			t.Fatal(err)
		}
		if seeds != nil {
			t.Error("expected nil seeds for nonexistent dir")
		}
	})
}

func TestFuzzCacheDir(t *testing.T) {
	dir, err := fuzzCacheDir(context.Background(), "example.com/pkg", "FuzzFoo")
	if err != nil {
		t.Fatal(err)
	}
	wantSuffix := filepath.Join("fuzz", "example.com", "pkg", "FuzzFoo")
	if !strings.HasSuffix(dir, wantSuffix) {
		t.Errorf("fuzzCacheDir = %q, doesn't end with expected suffix", dir)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
