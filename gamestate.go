package bgammon

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
