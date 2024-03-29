package bgammon

import (
	"log"
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
func NewBoard(variant int8) []int8 {
	space := make([]int8, BoardSpaces)
	switch variant {
	case VariantBackgammon:
		space[24], space[1] = 2, -2
		space[19], space[6] = -5, 5
		space[17], space[8] = -3, 3
		space[13], space[12] = 5, -5
	case VariantAceyDeucey, VariantTabula:
		space[SpaceHomePlayer], space[SpaceHomeOpponent] = 15, -15
	default:
		log.Panicf("failed to initialize board: unknown variant: %d", variant)
	}
	return space
}

// HomeRange returns the start and end space of the provided player's home board.
func HomeRange(player int8, variant int8) (from int8, to int8) {
	if player == 2 || variant == VariantTabula {
		return 24, 19
	}
	return 1, 6
}

// RollForMove returns the roll needed to move a checker from the provided spaces.
func RollForMove(from int8, to int8, player int8, variant int8) int8 {
	if !ValidSpace(from) || !ValidSpace(to) {
		return 0
	}

	// Handle standard moves.
	if from >= 1 && from <= 24 && to >= 1 && to <= 24 {
		return SpaceDiff(from, to, variant)
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
		if player == 2 && variant != VariantTabula {
			return 25 - to
		} else {
			return to
		}
	}
	return 0
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
