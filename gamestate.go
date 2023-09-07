package bgammon

import (
	"log"
)

type GameState struct {
	*Game
	PlayerNumber int
	Available    [][]int // Legal moves.
}

func (g *GameState) OpponentPlayer() Player {
	if g.PlayerNumber == 1 {
		return g.Player2
	}
	return g.Player1
}

func (g *GameState) LocalPlayer() Player {
	if g.PlayerNumber == 1 {
		return g.Player1
	}
	return g.Player2
}

func (g *GameState) SpaceAt(x int, y int) int {
	if (x < 0 || x > 12) || (y < 0 || y > 9) {
		return -1
	}

	space := -1
	if x <= 5 {
		space = x + 1
	} else if x == 6 {
		if y <= 4 {
			return SpaceBarOpponent
		} else {
			return SpaceBarPlayer
		}
	} else {
		space = x
	}

	if g.PlayerNumber == 2 {
		if y <= 4 {
			space = 25 - space
		}
	} else {
		if y <= 4 {
			space += 12
		} else {
			space = 13 - space
		}
	}
	return space
}

func (g *GameState) NeedRoll() bool {
	switch g.Turn {
	case 0:
		if g.PlayerNumber == 1 {
			return g.Player2.Name != "" && (g.Roll1 == 0 || (g.Roll1 == g.Roll2))
		} else if g.PlayerNumber == 2 {
			return g.Player1.Name != "" && (g.Roll2 == 0 || (g.Roll1 == g.Roll2))
		}
		return false
	case 1:
		return g.PlayerNumber == g.Turn && g.Player2.Name != "" && g.Roll1 == 0
	case 2:
		return g.PlayerNumber == g.Turn && g.Player1.Name != "" && g.Roll1 == 0
	default:
		log.Panicf("unknown turn %d", g.Turn)
		return false
	}
}

func (g *GameState) NeedOk() bool {
	return g.Turn != 0 && g.Turn == g.PlayerNumber && len(g.Available) == 0
}
