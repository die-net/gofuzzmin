# gofuzzmin

Minimize a Go fuzz corpus to the smallest set of shortest seeds that preserve
full edge coverage, analogous to
[afl-cmin](https://github.com/AFLplusplus/AFLplusplus) for AFL.

Go's built-in fuzzing grows its cache corpus monotonically and has no
minimization support ([golang/go#49290](https://github.com/golang/go/issues/49290)).
Over time corpora accumulate thousands of redundant seeds that slow down future
fuzzing runs. `gofuzzmin` fixes this.

## Install

Pre-built binaries for Linux, macOS, and Windows are available on the
[releases page](https://github.com/die-net/gofuzzmin/releases).

Or install from source:

```
go install github.com/die-net/gofuzzmin@latest
```

## Usage

```
gofuzzmin [flags] [package]
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `-func` | auto-detect | Fuzz function name |
| `-prune` | false | Delete non-selected seeds from cache |
| `-v` | false | Verbose output |
| `-timeout` | 30s | Warmup phase timeout |

### Examples

Dry run (report only):

```
gofuzzmin ./foo
```

Minimize and prune:

```
gofuzzmin -prune ./foo
```

Specify the fuzz function when there are multiple:

```
gofuzzmin -func FuzzBar -prune ./foo
```

### Example output

```
found 5831 cache seeds for FuzzBaz (package github.com/bar)
built test binary (0.3s)
collected coverage for 5831 seeds (3.2s)
total coverage bits: 19287
minimal corpus: 3212 seeds (55.1%), 2619 removable
run with -prune to delete removable seeds from cache
```

## How it works

1. Discovers the fuzz cache corpus via `go env GOCACHE`
2. Builds a test binary with the fuzzer's own edge-counter instrumentation
   (`go test -c -gcflags=-d=libfuzzer`)
3. Sorts cache seeds by size (shortest first) so shorter seeds are preferred
4. Runs the fuzzer's warmup phase with `GODEBUG=fuzzdebug=1` and
   `-test.parallel=1` in a single invocation, parsing the `new bits` reported
   for each seed
5. Seeds that contribute zero new coverage bits (their coverage is already
   provided by shorter seeds processed earlier) are marked removable
6. With `-prune`, deletes the redundant seeds from the cache

This uses the **exact same coverage metric** the fuzzer uses internally:
compiler-inserted 8-bit edge counters with power-of-two bucketing. This
matters because Go's `-coverprofile` measures a different, coarser signal
(statement-level set coverage) that doesn't match what the fuzzer considers
"interesting."

Only the build cache corpus is touched. Seeds in `testdata/fuzz/` are never
deleted — they serve as regression tests.

## Development

Run tests (use `-short` to skip the integration test):

```
go test ./...
```

Run the linter ([golangci-lint v2](https://golangci-lint.run/)):

```
golangci-lint run .
```

## License

BSD 3-Clause. See [LICENSE](LICENSE).
