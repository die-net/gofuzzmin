package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type corpusInfo struct {
	funcName   string
	importPath string
	pkgDir     string
	cacheDir   string
	cacheSeeds []seedFile
}

type seedFile struct {
	path     string // absolute path
	name     string // basename
	inputLen int    // approximate input length from parsing the corpus file
}

func discoverCorpus(ctx context.Context, pkg, funcName string) (*corpusInfo, error) {
	importPath, pkgDir, err := resolvePackage(ctx, pkg)
	if err != nil {
		return nil, fmt.Errorf("resolving package %q: %w", pkg, err)
	}

	if funcName == "" {
		funcName, err = detectFuzzFunc(pkgDir)
		if err != nil {
			return nil, err
		}
	}

	cacheDir, err := fuzzCacheDir(ctx, importPath, funcName)
	if err != nil {
		return nil, err
	}

	seeds, err := enumerateSeeds(cacheDir)
	if err != nil {
		return nil, fmt.Errorf("enumerating cache seeds: %w", err)
	}

	return &corpusInfo{
		funcName:   funcName,
		importPath: importPath,
		pkgDir:     pkgDir,
		cacheDir:   cacheDir,
		cacheSeeds: seeds,
	}, nil
}

// resolvePackage returns the import path and on-disk directory for a Go package.
// pkg may be an import path ("./foo") or an absolute directory path.
func resolvePackage(ctx context.Context, pkg string) (importPath, pkgDir string, err error) {
	arg := pkg
	cmd := exec.CommandContext(ctx, "go", "list", "-f", "{{.ImportPath}}\n{{.Dir}}", arg) //nolint:gosec // args are not user-controlled
	// When pkg is an absolute directory, run `go list` from within it so
	// that module resolution works (important for packages inside testdata
	// or other directories Go tools would otherwise skip).
	if filepath.IsAbs(pkg) {
		cmd = exec.CommandContext(ctx, "go", "list", "-f", "{{.ImportPath}}\n{{.Dir}}", ".")
		cmd.Dir = pkg
	}
	out, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("go list %s: %w", pkg, err)
	}
	lines := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)
	if len(lines) != 2 {
		return "", "", fmt.Errorf("unexpected go list output: %q", string(out))
	}
	return lines[0], lines[1], nil
}

// fuzzCacheDir returns the path to the fuzz cache directory for the given
// import path and fuzz function name.
func fuzzCacheDir(ctx context.Context, importPath, funcName string) (string, error) {
	cmd := exec.CommandContext(ctx, "go", "env", "GOCACHE")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go env GOCACHE: %w", err)
	}
	goCache := strings.TrimSpace(string(out))
	return filepath.Join(goCache, "fuzz", importPath, funcName), nil
}

var fuzzFuncRe = regexp.MustCompile(`^func\s+(Fuzz\w+)\s*\(`)

// detectFuzzFunc scans _test.go files in pkgDir for fuzz function signatures.
// Returns the name if exactly one is found; errors otherwise.
func detectFuzzFunc(pkgDir string) (string, error) {
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return "", fmt.Errorf("reading package dir: %w", err)
	}

	var found []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		names, err := scanFuzzFuncs(filepath.Join(pkgDir, e.Name()))
		if err != nil {
			return "", err
		}
		found = append(found, names...)
	}

	switch len(found) {
	case 0:
		return "", fmt.Errorf("no fuzz functions found in %s", pkgDir)
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("multiple fuzz functions found (%s); use -func to specify one", strings.Join(found, ", "))
	}
}

func scanFuzzFuncs(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var found []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if m := fuzzFuncRe.FindStringSubmatch(sc.Text()); m != nil {
			found = append(found, m[1])
		}
	}
	return found, sc.Err()
}

// enumerateSeeds lists all seed files in dir and parses their approximate
// input length from the Go fuzz corpus file format.
func enumerateSeeds(dir string) ([]seedFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	seeds := make([]seedFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name())
		inputLen := parseFuzzFileLen(p)
		seeds = append(seeds, seedFile{
			path:     p,
			name:     e.Name(),
			inputLen: inputLen,
		})
	}
	return seeds, nil
}

// parseFuzzFileLen reads a Go fuzz corpus file and returns the approximate
// total byte length of all encoded values. On any error, returns the file size.
func parseFuzzFileLen(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	// Skip the header line ("go test fuzz v1\n") and sum the remaining
	// content length as an approximation.
	s := string(data)
	if idx := strings.Index(s, "\n"); idx >= 0 {
		return len(s) - idx - 1
	}
	return len(data)
}
