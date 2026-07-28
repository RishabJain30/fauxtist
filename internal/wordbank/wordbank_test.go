package wordbank

import (
	"math/rand"
	"testing"
)

func TestPickReturnsPair(t *testing.T) {
	wb := New(rand.New(rand.NewSource(1)))
	cat, word, ok := wb.Pick(map[string]bool{})
	if !ok {
		t.Fatal("Pick returned ok=false on a fresh bank")
	}
	if cat == "" || word == "" {
		t.Fatalf("empty pair: %q/%q", cat, word)
	}
}

func TestPickExcludesUsed(t *testing.T) {
	wb := New(rand.New(rand.NewSource(1)))
	used := map[string]bool{}
	// Exhaust the bank; every returned word must be new.
	for i := 0; i < wb.Len(); i++ {
		_, word, ok := wb.Pick(used)
		if !ok {
			t.Fatalf("Pick failed at %d with capacity %d", i, wb.Len())
		}
		if used[word] {
			t.Fatalf("Pick returned already-used word %q", word)
		}
		used[word] = true
	}
	// Now everything is used -> ok=false.
	if _, _, ok := wb.Pick(used); ok {
		t.Fatal("expected ok=false when all words used")
	}
}
