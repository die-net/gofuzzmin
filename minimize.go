package main

// minimize returns the subset of seeds that contributed new coverage bits
// during warmup. Seeds with newBits == 0 are redundant (their coverage is
// already provided by shorter/earlier seeds in the sorted order).
func minimize(results []seedResult) (selected []seedResult) {
	for i := range results {
		if results[i].newBits > 0 {
			selected = append(selected, results[i])
		}
	}
	return selected
}
