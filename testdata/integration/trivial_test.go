package integration

import (
	"testing"
)

func FuzzTrivial(f *testing.F) {
	f.Add("hello")
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		_ = process(s)
	})
}
