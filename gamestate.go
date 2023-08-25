package bgammon

type GameState struct {
	*Game
	Player    int
	Available [][]int // Legal moves.
}

func (g *GameState) OpponentPlayer() Player {
	if g.Player == 1 {
		return g.Player2
	}
	return g.Player1
}

func (g *GameState) LocalPlayer() Player {
	if g.Player == 1 {
		return g.Player1
	}
	return g.Player2
}
