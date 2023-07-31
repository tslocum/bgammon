package bgammon

// board is stored on server from blacks perspective of 1-24
// all state sent to white, and input received from white is reversed
// handle this transparently by translating at the message level rather than each time spaces are used

// 1-24 for 24 spaces, 2 spaces for bar, 2 spaces for home
const (
	SpaceHomePlayer   = 0
	SpaceBarPlayer    = 25
	SpaceBarOpponent  = 26
	SpaceHomeOpponent = 27
)

const numBoardSpaces = 28

type Board struct {
	Space []int // Positive values represent player 1 (black), negative values represent player 2 (white).
}

func NewBoard() *Board {
	return &Board{
		Space: make([]int, numBoardSpaces),
	}
}
