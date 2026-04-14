// Command gofuzzmin minimizes a Go fuzz corpus to the smallest set of
// shortest seeds that preserve full edge coverage.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"
)

func main() {
	var cfg config
	flag.StringVar(&cfg.funcName, "func", "", "fuzz function name (auto-detected if only one exists)")
	flag.BoolVar(&cfg.prune, "prune", false, "delete non-selected seeds from cache")
	flag.BoolVar(&cfg.verbose, "v", false, "verbose output")
	flag.DurationVar(&cfg.timeout, "timeout", 30*time.Second, "warmup phase timeout")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: gofuzzmin [flags] [package]\n\n")
		fmt.Fprint(os.Stderr, "Minimize a Go fuzz corpus by selecting the smallest set of shortest\n")
		fmt.Fprint(os.Stderr, "seeds that preserve full edge coverage.\n\n")
		fmt.Fprint(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	pkg := "."
	if flag.NArg() > 0 {
		pkg = flag.Arg(0)
	}
	cfg.pkg = pkg

	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "gofuzzmin: %v\n", err)
		os.Exit(1)
	}
}

func run(cfg config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	t0 := time.Now()

	// Step 1: Discover corpus.
	corpus, err := discoverCorpus(ctx, cfg.pkg, cfg.funcName)
	if err != nil {
		return err
	}
	if len(corpus.cacheSeeds) == 0 {
		fmt.Println("no cache seeds found; nothing to minimize")
		return nil
	}
	fmt.Printf("found %d cache seeds for %s (package %s)\n", len(corpus.cacheSeeds), corpus.funcName, corpus.importPath)

	// Step 2: Build test binary.
	t1 := time.Now()
	binPath, cleanup, err := buildTestBinary(ctx, cfg.pkg)
	if err != nil {
		return err
	}
	defer cleanup()
	fmt.Printf("built test binary (%.1fs)\n", time.Since(t1).Seconds())

	// Step 3: Collect coverage via fuzzer warmup.
	t2 := time.Now()
	results, totalCovBits, err := collectCoverage(ctx, cfg, corpus, binPath)
	if err != nil {
		return err
	}
	fmt.Printf("collected coverage for %d seeds (%.1fs)\n",
		len(results), time.Since(t2).Seconds())

	// Step 4: Minimize.
	selected := minimize(results)
	removable := len(results) - len(selected)

	fmt.Printf("total coverage bits: %d\n", totalCovBits)
	fmt.Printf("minimal corpus: %d seeds (%.1f%%), %d removable\n",
		len(selected), 100*float64(len(selected))/float64(len(results)), removable)

	if cfg.verbose {
		for _, s := range selected {
			fmt.Printf("  keep: %s (%d bytes, %d new bits)\n", s.name, s.fileLen, s.newBits)
		}
	}

	// Step 5: Prune or report.
	if cfg.prune {
		pruned, err := pruneCache(corpus, selected)
		if err != nil {
			return err
		}
		fmt.Printf("deleted %d seeds from cache\n", pruned)
	} else if removable > 0 {
		fmt.Println("run with -prune to delete removable seeds from cache")
	}

	if cfg.verbose {
		fmt.Printf("total time: %.1fs\n", time.Since(t0).Seconds())
	}

	return nil
}
