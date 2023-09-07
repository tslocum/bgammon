package bgammon

// board is stored on server from blacks perspective of 1-24
// all state sent to white, and input received from white is reversed
// handle this transparently by translating at the message level rather than each time spaces are used

// HomePlayer is the real Player1 home, HomeOpponent is the real Player2 home
// HomeBoardPlayer (Player1) ranges 1-6, HomeBoardOpponent (Player2) ranges 24-19 (visible to them as 1-6)

// 1-24 for 24 spaces, 2 spaces for bar, 2 spaces for home
const (
	SpaceHomePlayer   = 0
	SpaceHomeOpponent = 25
	SpaceBarPlayer    = 26
	SpaceBarOpponent  = 27
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

func RollForMove(from int, to int, player int) int {
	if !ValidSpace(from) || !ValidSpace(to) {
		return 0
	}

	// Handle standard moves.
	if from >= 1 && from <= 24 && to >= 1 && to <= 24 {
		return SpaceDiff(from, to)
	}

	playerHome := SpaceHomePlayer
	playerBar := SpaceBarPlayer
	oppHome := SpaceHomeOpponent
	oppBar := SpaceBarOpponent
	if player == 2 {
		playerHome, oppHome, playerBar, oppBar = oppHome, playerHome, oppBar, playerBar
	}

	// Handle moves with special 'to' space.
	if to == playerBar || to == oppBar || to == oppHome {
		return 0
	} else if to == playerHome {

	}

	// Handle moves with special 'from' space.
	if from == SpaceBarPlayer {
		if player == 2 {
			return 25 - to
		} else {
			return to
		}
	}
	return 0
}

func CanBearOff(board []int, player int) bool {
	homeStart, homeEnd := HomeRange(player)
	homeStart, homeEnd = minInt(homeStart, homeEnd), maxInt(homeStart, homeEnd)

	ok := true
	for i := 1; i < 24; i++ {
		if (i < homeStart || i > homeEnd) && PlayerCheckers(board[i], player) > 0 {
			ok = false
			break
		}
	}
	if ok && (PlayerCheckers(board[SpaceBarPlayer], player) > 0 || PlayerCheckers(board[SpaceBarOpponent], player) > 0) {
		ok = false
	}
	return ok
}
