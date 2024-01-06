package bgammon

import (
	"sort"
	"strconv"
	"strings"
)

const (
	SpaceHomePlayer   int8 = 0  // Current player's home.
	SpaceHomeOpponent int8 = 25 // Opponent player's home.
	SpaceBarPlayer    int8 = 26 // Current player's bar.
	SpaceBarOpponent  int8 = 27 // Opponent player's bar.
)

// BoardSpaces is the total number of spaces needed to represent a backgammon board.
const BoardSpaces = 28

// NewBoard returns a new backgammon board represented as integers. Positive
// integers represent player 1's checkers and negative integers represent
// player 2's checkers. The board's space numbering is always from the
// perspective of the current player (i.e. the 1 space will always be in the
// current player's home board).
func NewBoard(acey bool) []int8 {
	space := make([]int8, BoardSpaces)
	if acey {
		space[SpaceHomePlayer], space[SpaceHomeOpponent] = 15, -15
	} else {
		space[24], space[1] = 2, -2
		space[19], space[6] = -5, 5
		space[17], space[8] = -3, 3
		space[13], space[12] = 5, -5
	}
	return space
}

// HomeRange returns the start and end space of the provided player's home board.
func HomeRange(player int8) (from int8, to int8) {
	if player == 2 {
		return 24, 19
	}
	return 1, 6
}

// RollForMove returns the roll needed to move a checker from the provided spaces.
func RollForMove(from int8, to int8, player int8, acey bool) int8 {
	if !ValidSpace(from) || !ValidSpace(to) {
		return 0
	}

	// Handle standard moves.
	if from >= 1 && from <= 24 && to >= 1 && to <= 24 {
		return SpaceDiff(from, to, acey)
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

// CanBearOff returns whether the provided player can bear checkers off of the board.
func CanBearOff(board []int8, player int8, local bool) bool {
	if PlayerCheckers(board[SpaceBarPlayer], player) > 0 || PlayerCheckers(board[SpaceBarOpponent], player) > 0 {
		return false
	}

	homeStart, homeEnd := int8(1), int8(6)
	if !local {
		homeStart, homeEnd = HomeRange(player)
		homeStart, homeEnd = minInt(homeStart, homeEnd), maxInt(homeStart, homeEnd)
	}
	for i := int8(1); i <= 24; i++ {
		if (i < homeStart || i > homeEnd) && PlayerCheckers(board[i], player) > 0 {
			return false
		}
	}
	return true
}

func ParseSpace(space string) int8 {
	i, err := strconv.Atoi(space)
	if err != nil {
		switch strings.ToLower(space) {
		case "bar", "b":
			return SpaceBarPlayer
		case "off", "o", "home", "h":
			return SpaceHomePlayer
		}
		return -1
	}
	return int8(i)
}

func compareMoveFunc(moves [][]int8) func(i, j int) bool {
	return func(i, j int) bool {
		if moves[j][0] == moves[i][0] {
			return moves[j][1] < moves[i][1]
		}
		return moves[j][0] < moves[i][0]
	}
}

// SortMoves sorts moves from highest to lowest.
func SortMoves(moves [][]int8) {
	sort.Slice(moves, compareMoveFunc(moves))
}
