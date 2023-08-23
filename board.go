package bgammon

// board is stored on server from blacks perspective of 1-24
// all state sent to white, and input received from white is reversed
// handle this transparently by translating at the message level rather than each time spaces are used

// HomePlayer is the real Player1 home, HomeOpponent is the real Player2 home
// HomeBoardPlayer (Player1) ranges 1-6, HomeBoardOpponent (Player2) ranges 24-19 (visible to them as 1-6)

// 1-24 for 24 spaces, 2 spaces for bar, 2 spaces for home
const (
	SpaceHomePlayer   = 0
	SpaceBarPlayer    = 25
	SpaceBarOpponent  = 26
	SpaceHomeOpponent = 27
)

const BoardSpaces = 28

func NewBoard() []int {
	space := make([]int, BoardSpaces)
	space[24], space[1] = 2, -2
	space[19], space[6] = -5, 5
	space[17], space[8] = -3, 3
	space[13], space[12] = 5, -5
	return space
}

func HomeRange(player int) (from int, to int) {
	if player == 2 {
		return 24, 19
	}
	return 1, 6
}
