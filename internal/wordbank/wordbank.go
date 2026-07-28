package wordbank

import "math/rand"

type pair struct{ category, word string }

// WordBank is a static, in-memory source of category/word pairs. It satisfies
// game.WordSource without importing the game package (structural typing).
type WordBank struct {
	pairs []pair
	rng   *rand.Rand
}

// New returns a WordBank seeded with the built-in pack.
func New(rng *rand.Rand) *WordBank {
	return &WordBank{pairs: defaultPairs(), rng: rng}
}

// Len is the total number of pairs, useful for tests.
func (wb *WordBank) Len() int { return len(wb.pairs) }

// Pick returns a random pair whose word is not in exclude. ok is false if none
// remain.
func (wb *WordBank) Pick(exclude map[string]bool) (string, string, bool) {
	var avail []pair
	for _, p := range wb.pairs {
		if !exclude[p.word] {
			avail = append(avail, p)
		}
	}
	if len(avail) == 0 {
		return "", "", false
	}
	p := avail[wb.rng.Intn(len(avail))]
	return p.category, p.word, true
}

func defaultPairs() []pair {
	return []pair{
		{"Animal", "Giraffe"}, {"Animal", "Octopus"}, {"Animal", "Penguin"},
		{"Animal", "Kangaroo"}, {"Animal", "Hedgehog"}, {"Animal", "Dolphin"},
		{"Food", "Pizza"}, {"Food", "Sushi"}, {"Food", "Pancakes"},
		{"Food", "Popcorn"}, {"Food", "Spaghetti"}, {"Food", "Cupcake"},
		{"Object", "Umbrella"}, {"Object", "Telescope"}, {"Object", "Anchor"},
		{"Object", "Lighthouse"}, {"Object", "Hourglass"}, {"Object", "Compass"},
		{"Place", "Volcano"}, {"Place", "Desert"}, {"Place", "Castle"},
		{"Place", "Waterfall"}, {"Place", "Igloo"}, {"Place", "Windmill"},
		{"Sport", "Surfing"}, {"Sport", "Bowling"}, {"Sport", "Archery"},
		{"Sport", "Skateboarding"}, {"Sport", "Fencing"}, {"Sport", "Basketball"},
	}
}
