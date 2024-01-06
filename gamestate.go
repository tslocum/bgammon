package bgammon

import (
	"log"
)

type GameState struct {
	*Game
	PlayerNumber int8
	Available    [][]int8 // Legal moves.
	Spectating   bool
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

func (g *GameState) SpaceAt(x int8, y int8) int8 {
	if (x < 0 || x > 12) || (y < 0 || y > 9) {
		return -1
	}

	var space int8 = -1
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

// Pips returns the pip count for the specified player.
func (g *GameState) Pips(player int8) int {
	var pips int
	var spaceValue int
	if player == 1 {
		pips += int(PlayerCheckers(g.Board[SpaceBarPlayer], player)) * 25
	} else {
		pips += int(PlayerCheckers(g.Board[SpaceBarOpponent], player)) * 25
	}
	if g.Acey {
		if player == 1 && !g.Player1.Entered {
			pips += int(PlayerCheckers(g.Board[SpaceHomePlayer], player)) * 25
		} else if player == 2 && !g.Player2.Entered {
			pips += int(PlayerCheckers(g.Board[SpaceHomeOpponent], player)) * 25
		}
	}
	for i := 1; i < 25; i++ {
		if player == g.PlayerNumber {
			spaceValue = i
		} else {
			spaceValue = 25 - i
		}
		pips += int(PlayerCheckers(g.Board[i], player)) * spaceValue
	}
	return pips
}

// MayDouble returns whether the player may send the 'double' command.
func (g *GameState) MayDouble() bool {
	if g.Spectating || g.Winner != 0 || g.Acey {
		return false
	}
	return g.Points != 1 && g.Turn != 0 && g.Turn == g.PlayerNumber && g.Roll1 == 0 && !g.DoubleOffered && (g.DoublePlayer == 0 || g.DoublePlayer == g.PlayerNumber)
}

// MayRoll returns whether the player may send the 'roll' command.
func (g *GameState) MayRoll() bool {
	if g.Spectating || g.Winner != 0 || g.DoubleOffered {
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

// MayChooseRoll returns whether the player may send the 'ok' command, supplying
// the chosen roll. This command only applies to acey-deucey games.
func (g *GameState) MayChooseRoll() bool {
	return g.Acey && g.Turn != 0 && g.Turn == g.PlayerNumber && ((g.Roll1 == 1 && g.Roll2 == 2) || (g.Roll1 == 2 && g.Roll2 == 1))
}

// MayOK returns whether the player may send the 'ok' command.
func (g *GameState) MayOK() bool {
	if g.Spectating || g.Winner != 0 {
		return false
	} else if g.Turn != 0 && g.Turn != g.PlayerNumber && g.PlayerNumber != g.DoublePlayer && g.DoubleOffered {
		return true
	}
	return g.Turn != 0 && g.Turn == g.PlayerNumber && g.Roll1 != 0 && len(g.Available) == 0
}

// MayResign returns whether the player may send the 'resign' command.
func (g *GameState) MayResign() bool {
	if g.Spectating || g.Winner != 0 {
		return false
	}
	return g.Turn != 0 && g.Turn != g.PlayerNumber && g.PlayerNumber != g.DoublePlayer && g.DoubleOffered
}

// MayReset returns whether the player may send the 'reset' command.
func (g *GameState) MayReset() bool {
	if g.Spectating || g.Winner != 0 {
		return false
	}
	return g.Turn != 0 && g.Turn == g.PlayerNumber && len(g.Moves) > 0
}
