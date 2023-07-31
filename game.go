package bgammon

import "math/rand"

type Game struct {
	Board   *Board
	Player1 *Player
	Player2 *Player
	Turn    int
	Roll1   int
	Roll2   int
}

func NewGame() *Game {
	return &Game{
		Board: NewBoard(),
	}
}

func (g *Game) roll(r rand.Rand, player int) {
	if player != g.Turn || g.Roll1 != 0 || g.Roll2 != 0 {
		return
	}
	g.Roll1, g.Roll2 = r.Intn(6)+1, r.Intn(6)+1
}

func (g *Game) LegalMoves() []int {
	// todo get current player based on turn and enumerate spaces and roll to get available moves
	// sent to clients and used to validate moves
	var moves []int
	return moves
}
