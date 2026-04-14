package main

import "testing"

func TestMinimize(t *testing.T) {
	t.Run("filters zero-bit seeds", func(t *testing.T) {
		results := []seedResult{
			{name: "a", newBits: 5, fileLen: 10},
			{name: "b", newBits: 0, fileLen: 10},
			{name: "c", newBits: 3, fileLen: 20},
			{name: "d", newBits: 0, fileLen: 15},
		}
		selected := minimize(results)
		if len(selected) != 2 {
			t.Fatalf("len(selected) = %d, want 2", len(selected))
		}
		if selected[0].name != "a" || selected[1].name != "c" {
			t.Errorf("selected = [%s, %s], want [a, c]", selected[0].name, selected[1].name)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		selected := minimize(nil)
		if len(selected) != 0 {
			t.Error("expected empty result")
		}
	})

	t.Run("all redundant", func(t *testing.T) {
		results := []seedResult{
			{name: "a", newBits: 0, fileLen: 10},
			{name: "b", newBits: 0, fileLen: 20},
		}
		selected := minimize(results)
		if len(selected) != 0 {
			t.Errorf("expected empty result, got %d", len(selected))
		}
	})
}
