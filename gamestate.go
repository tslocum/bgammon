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

// MayDouble returns whether the player may send the 'double' command.
func (g *GameState) MayDouble() bool {
	if g.Winner != 0 {
		return false
	}
	return g.Points != 1 && g.Turn != 0 && g.Turn == g.PlayerNumber && g.Roll1 == 0 && !g.DoubleOffered && (g.DoublePlayer == 0 || g.DoublePlayer == g.PlayerNumber)
}

// MayRoll returns whether the player may send the 'roll' command.
func (g *GameState) MayRoll() bool {
	if g.Winner != 0 || g.DoubleOffered {
		return false
	}
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

// MayOK returns whether the player may send the 'ok' command.
func (g *GameState) MayOK() bool {
	if g.Winner != 0 {
		return false
	} else if g.Turn != 0 && g.Turn != g.PlayerNumber && g.DoubleOffered {
		return true
	}
	return g.Turn != 0 && g.Turn == g.PlayerNumber && g.Roll1 != 0 && len(g.Available) == 0
}

// MayResign returns whether the player may send the 'resign' command.
func (g *GameState) MayResign() bool {
	if g.Winner != 0 {
		return false
	}
	return g.Turn != 0 && g.Turn != g.PlayerNumber && g.DoubleOffered
}

// MayReset returns whether the player may send the 'reset' command.
func (g *GameState) MayReset() bool {
	if g.Winner != 0 {
		return false
	}
	return g.Turn != 0 && g.Turn == g.PlayerNumber && len(g.Moves) > 0
}
