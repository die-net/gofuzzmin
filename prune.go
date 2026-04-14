package main

import (
	"fmt"
	"os"
)

// pruneCache deletes cache seeds that are not in the selected set.
// Returns the number of seeds deleted.
func pruneCache(corpus *corpusInfo, selected []seedResult) (int, error) {
	keep := make(map[string]struct{}, len(selected))
	for _, s := range selected {
		keep[s.name] = struct{}{}
	}

	var deleted int
	for _, seed := range corpus.cacheSeeds {
		if _, ok := keep[seed.name]; ok {
			continue
		}
		if err := os.Remove(seed.path); err != nil {
			return deleted, fmt.Errorf("removing %s: %w", seed.path, err)
		}
		deleted++
	}
	return deleted, nil
}
