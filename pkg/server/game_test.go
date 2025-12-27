package server

import (
	"testing"

	"codeberg.org/tslocum/bgammon"
)

func TestServerGameWinPoints(t *testing.T) {
	t.Parallel()

	type testCase struct {
		variant   int8
		board     []int8
		expected1 int8
		expected2 int8
	}

	var testCases = []*testCase{
		{
			variant:   bgammon.VariantBackgammon,
			expected1: 3,
			expected2: 3,
		}, {
			variant:   bgammon.VariantAceyDeucey,
			expected1: 15,
			expected2: 15,
		}, {
			variant:   bgammon.VariantTabula,
			expected1: 3,
			expected2: 3,
		},
	}
	for i, c := range testCases {
		g := newServerGame(1, c.variant)
		points1 := g.winPoints(1)
		points2 := g.winPoints(2)
		if points1 != c.expected1 {
			t.Fatalf("unexpected player 1 winPoints for case %d board %v: expected %d, got %d", i, g.Board, c.expected1, points1)
		} else if points2 != c.expected2 {
			t.Fatalf("unexpected player 2 winPoints for case %d board %v: expected %d, got %d", i, g.Board, c.expected2, points2)
		}
	}
}
