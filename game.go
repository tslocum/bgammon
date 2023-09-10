package bgammon

import (
	"bytes"
	"fmt"
	"log"
	"strconv"
)

var boardTopBlack = []byte("+13-14-15-16-17-18-+---+19-20-21-22-23-24-+")
var boardBottomBlack = []byte("+12-11-10--9--8--7-+---+-6--5--4--3--2--1-+")

var boardTopWhite = []byte("+24-23-22-21-20-19-+---+18-17-16-15-14-13-+")
var boardBottomWhite = []byte("+-1--2--3--4--5--6-+---+-7--8--9-10-11-12-+")

type Game struct {
	Board   []int
	Player1 Player
	Player2 Player
	Turn    int
	Winner  int
	Roll1   int
	Roll2   int
	Moves   [][]int // Pending moves.

	boardStates [][]int // One board state for each move to allow undoing a move.
}

func NewGame() *Game {
	return &Game{
		Board:   NewBoard(),
		Player1: NewPlayer(1),
		Player2: NewPlayer(2),
	}
}

func (g *Game) Copy() *Game {
	newGame := &Game{
		Board:       make([]int, len(g.Board)),
		Player1:     g.Player1,
		Player2:     g.Player2,
		Turn:        g.Turn,
		Winner:      g.Winner,
		Roll1:       g.Roll1,
		Roll2:       g.Roll2,
		Moves:       make([][]int, len(g.Moves)),
		boardStates: make([][]int, len(g.boardStates)),
	}
	copy(newGame.Board, g.Board)
	copy(newGame.Moves, g.Moves)
	copy(newGame.boardStates, g.boardStates)
	return newGame
}

func (g *Game) NextTurn() {
	if g.Winner != 0 {
		return
	}

	nextTurn := 1
	if g.Turn == 1 {
		nextTurn = 2
	}
	g.Roll1, g.Roll2 = 0, 0
	g.Turn = nextTurn
	g.Moves = g.Moves[:0]
	g.boardStates = g.boardStates[:0]
}

func (g *Game) turnPlayer() Player {
	switch g.Turn {
	case 2:
		return g.Player2
	default:
		return g.Player1
	}
}

func (g *Game) opponentPlayer() Player {
	switch g.Turn {
	case 2:
		return g.Player1
	default:
		return g.Player2
	}
}

func (g *Game) iterateSpaces(from int, to int, f func(space int, spaceCount int)) {
	if from == to || from < 1 || from > 24 || to < 1 || to > 24 {
		return
	}

	i := 1
	if to > from {
		for space := from; space <= to; space++ {
			f(space, i)
			i++
		}
	} else {
		for space := from; space >= to; space-- {
			f(space, i)
			i++
		}
	}
}

func (g *Game) addMove(move []int) bool {
	opponentCheckers := OpponentCheckers(g.Board[move[1]], g.Turn)
	if opponentCheckers > 1 {
		return false
	}

	delta := 1
	if g.Turn == 2 {
		delta = -1
	}

	boardState := make([]int, len(g.Board))
	copy(boardState, g.Board)
	g.boardStates = append(g.boardStates, boardState)

	g.Board[move[0]] -= delta
	if opponentCheckers == 1 { // Hit checker.
		g.Board[move[1]] = delta

		// Move opponent checker to bar.
		barSpace := SpaceBarOpponent
		if g.Turn == 2 {
			barSpace = SpaceBarPlayer
		}
		g.Board[barSpace] += delta * -1
	} else {
		g.Board[move[1]] += delta
	}

	g.Moves = append(g.Moves, []int{move[0], move[1]})
	return true
}

// AddMoves adds moves to the game state.  Adding a backwards move will remove the equivalent existing move.
func (g *Game) AddMoves(moves [][]int) bool {
	if g.Player1.Name == "" || g.Player2.Name == "" || g.Winner != 0 {
		return false
	}

	var addMoves [][]int
	var undoMoves [][]int

	gameCopy := g.Copy()

	validateOffset := 0
VALIDATEMOVES:
	for _, move := range moves {
		l := gameCopy.LegalMoves()
		for _, lm := range l {
			if lm[0] == move[0] && lm[1] == move[1] {
				addMoves = append(addMoves, []int{move[0], move[1]})
				continue VALIDATEMOVES
			}
		}
		if len(gameCopy.Moves) > 0 {
			i := len(gameCopy.Moves) - 1 - validateOffset
			if i < 0 {
				return false
			}
			gameMove := gameCopy.Moves[i]
			if move[0] == gameMove[1] && move[1] == gameMove[0] {
				undoMoves = append(undoMoves, []int{gameMove[1], gameMove[0]})
				validateOffset++
				continue VALIDATEMOVES
			}
		}
		return false
	}

	if len(addMoves) != 0 && len(undoMoves) != 0 {
		return false
	}

	var checkWin bool
ADDMOVES:
	for _, move := range addMoves {
		l := gameCopy.LegalMoves()
		for _, lm := range l {
			if lm[0] == move[0] && lm[1] == move[1] {
				if !gameCopy.addMove(move) {
					return false
				}

				if move[1] == SpaceHomePlayer || move[1] == SpaceHomeOpponent {
					checkWin = true
				}
				continue ADDMOVES
			}
		}
	}
	for _, move := range undoMoves {
		if len(gameCopy.Moves) > 0 {
			i := len(gameCopy.Moves) - 1
			if i < 0 {
				return false
			}
			gameMove := gameCopy.Moves[i]
			if move[0] == gameMove[1] && move[1] == gameMove[0] {
				copy(gameCopy.Board, gameCopy.boardStates[i])
				gameCopy.boardStates = gameCopy.boardStates[:i]

				gameCopy.Moves = gameCopy.Moves[:i]
				continue
			}
		}
		return false
	}

	g.Board = gameCopy.Board
	g.Moves = gameCopy.Moves
	g.boardStates = gameCopy.boardStates

	if checkWin {
		var foundChecker bool
		for space := 1; space <= 24; space++ {
			if PlayerCheckers(g.Board[space], g.Turn) != 0 {
				foundChecker = true
				break
			}
		}
		if !foundChecker {
			g.Winner = g.Turn
		}
	}

	return true
}

func (g *Game) LegalMoves() [][]int {
	if g.Winner != 0 || g.Roll1 == 0 || g.Roll2 == 0 {
		return nil
	}

	rolls := []int{
		g.Roll1,
		g.Roll2,
	}
	if g.Roll1 == g.Roll2 { // Rolled doubles.
		rolls = append(rolls, g.Roll1, g.Roll2)
	}

	haveDiceRoll := func(from, to int) int {
		// TODO diff needs to account for bar and home special spaces
		diff := SpaceDiff(from, to)
		var c int
		for _, roll := range rolls {
			if roll == diff {
				c++
			}
		}
		return c
	}

	haveBearOffDiceRoll := func(diff int) int {
		var c int
		for _, roll := range rolls {
			if roll >= diff {
				c++
			}
		}
		return c
	}

	useDiceRoll := func(from, to int) {
		if to == SpaceHomePlayer || to == SpaceHomeOpponent {
			needRoll := from
			if to == SpaceHomeOpponent {
				needRoll = 25 - from
			}
			for i, roll := range rolls {
				if roll >= needRoll {
					rolls = append(rolls[:i], rolls[i+1:]...)
					return
				}
			}
			log.Panicf("no dice roll to use for %d/%d", from, to)
		}

		diff := SpaceDiff(from, to)
		for i, roll := range rolls {
			if roll == diff {
				rolls = append(rolls[:i], rolls[i+1:]...)
				return
			}
		}
	}

	for _, move := range g.Moves {
		useDiceRoll(move[0], move[1])
	}

	var moves [][]int

	barSpace := SpaceBarPlayer
	if g.Turn == 2 {
		barSpace = SpaceBarOpponent
	}
	mustEnter := g.Board[barSpace] != 0

	if mustEnter { // Must enter from bar.
		from, to := HomeRange(g.opponentPlayer().Number)
		g.iterateSpaces(from, to, func(homeSpace int, spaceCount int) {
			available := haveDiceRoll(barSpace, homeSpace)
			if available == 0 {
				return
			}
			opponentCheckers := OpponentCheckers(g.Board[homeSpace], g.Turn)
			if opponentCheckers <= 1 {
				movable := PlayerCheckers(g.Board[barSpace], g.Turn)
				if movable > available {
					movable = available
				}
				for i := 0; i < movable; i++ {
					moves = append(moves, []int{barSpace, homeSpace})
				}
			}
		})
	} else {
		canBearOff := CanBearOff(g.Board, g.Turn)
		for space := range g.Board {
			if space == SpaceBarPlayer || space == SpaceBarOpponent { // Handled above.
				continue
			} else if space == SpaceHomePlayer || space == SpaceHomeOpponent { // No entering from home spaces (until acey-deucey is added).
				continue
			}

			checkers := g.Board[space]
			playerCheckers := PlayerCheckers(checkers, g.Turn)
			if playerCheckers == 0 {
				continue
			}

			if canBearOff {
				homeSpace := SpaceHomePlayer
				if g.Turn == 2 {
					homeSpace = SpaceHomeOpponent
				}
				available := haveBearOffDiceRoll(SpaceDiff(space, homeSpace))
				if available > 0 {
					movable := playerCheckers
					if movable > available {
						movable = available
					}
					for i := 0; i < movable; i++ {
						moves = append(moves, []int{space, homeSpace})
					}
				}
			}

			// Move normally.
			lastSpace := 1
			dir := -1
			if g.Turn == 2 {
				lastSpace = 24
				dir = 1
			}

			g.iterateSpaces(space+dir, lastSpace, func(to int, spaceCount int) {
				available := haveDiceRoll(space, to)
				if available == 0 {
					return
				}

				opponentCheckers := OpponentCheckers(g.Board[to], g.Turn)
				if opponentCheckers <= 1 {
					movable := playerCheckers
					if movable > available {
						movable = available
					}
					for i := 0; i < movable; i++ {
						moves = append(moves, []int{space, to})
					}
				}
			})
		}
	}

	// totalMoves tries all legal moves in a game and returns the maximum total number of moves that a player may consecutively make.
	var totalMoves func(in *Game, move []int) int
	totalMoves = func(in *Game, move []int) int {
		gc := in.Copy()
		if !gc.addMove(move) {
			log.Panicf("failed to add move %+v to game %+v", move, in)
		}

		maxTotal := 1
		for _, m := range gc.LegalMoves() {
			total := totalMoves(gc, m)
			if total+1 > maxTotal {
				maxTotal = total + 1
			}
		}
		return maxTotal
	}

	// Simulate all possible moves to their final value and only allow moves that will achieve the maximum total moves.
	var maxMoves int
	moveCounts := make([]int, len(moves))
	for i, move := range moves {
		moveCounts[i] = totalMoves(g, move)
		if moveCounts[i] > maxMoves {
			maxMoves = moveCounts[i]
		}
	}
	if maxMoves > 1 {
		var newMoves [][]int
		for i, move := range moves {
			if moveCounts[i] >= maxMoves {
				newMoves = append(newMoves, move)
			}
		}
		moves = newMoves
	}

	return moves
}

func (g *Game) RenderSpace(player int, space int, spaceValue int, legalMoves [][]int) []byte {
	var playerColor = "x"
	var opponentColor = "o"
	if player == 2 {
		playerColor = "o"
		opponentColor = "x"
	}

	var pieceColor string
	value := g.Board[space]
	if space == SpaceBarPlayer {
		pieceColor = playerColor
	} else if space == SpaceBarOpponent {
		pieceColor = opponentColor
	} else {
		if value < 0 {
			pieceColor = "o"
		} else if value > 0 {
			pieceColor = "x"
		} else {
			pieceColor = playerColor
		}
	}

	abs := value
	if value < 0 {
		abs = value * -1
	}

	top := space > 12
	if player == 2 {
		top = !top
	}

	firstDigit := 4
	secondDigit := 5
	if !top {
		firstDigit = 5
		secondDigit = 4
	}

	var firstNumeral string
	var secondNumeral string
	if abs > 5 {
		if abs > 9 {
			firstNumeral = "1"
		} else {
			firstNumeral = strconv.Itoa(abs)
		}
		if abs > 9 {
			secondNumeral = strconv.Itoa(abs - 10)
		}

		if spaceValue == firstDigit && (!top || abs > 9) {
			pieceColor = firstNumeral
		} else if spaceValue == secondDigit && abs > 9 {
			pieceColor = secondNumeral
		} else if top && spaceValue == secondDigit {
			pieceColor = firstNumeral
		}
	}

	if abs > 5 {
		abs = 5
	}

	var r []byte
	if abs > 0 && spaceValue <= abs {
		r = []byte(pieceColor)
	} else {
		r = []byte(" ")
	}
	return append(append([]byte(" "), r...), ' ')
}

func (g *Game) BoardState(player int) []byte {
	var t bytes.Buffer

	playerRating := "0"
	opponentRating := "0"

	var white bool
	if player == 2 {
		white = true
	}

	var opponentName = g.Player2.Name
	var playerName = g.Player1.Name
	if playerName == "" {
		playerName = "Waiting..."
	}
	if opponentName == "" {
		opponentName = "Waiting..."
	}
	if white {
		playerName, opponentName = opponentName, playerName
	}

	var playerColor = "x"
	var opponentColor = "o"
	playerRoll := g.Roll1
	opponentRoll := g.Roll2
	if white {
		playerColor = "o"
		opponentColor = "x"
		playerRoll = g.Roll2
		opponentRoll = g.Roll1
	}

	if white {
		t.Write(boardTopWhite)
	} else {
		t.Write(boardTopBlack)
	}
	t.WriteString(" ")
	t.WriteByte('\n')

	legalMoves := g.LegalMoves()
	space := func(row int, col int) []byte {
		spaceValue := row + 1
		if row > 5 {
			spaceValue = 5 - (row - 6)
		}

		if col == -1 {
			if row <= 4 {
				return g.RenderSpace(player, SpaceBarOpponent, spaceValue, legalMoves)
			}
			return g.RenderSpace(player, SpaceBarPlayer, spaceValue, legalMoves)
		}

		var space int
		if white {
			space = 24 - col
			if row > 5 {
				space = 1 + col
			}
		} else {
			space = 13 + col
			if row > 5 {
				space = 12 - col
			}
		}

		if row == 5 {
			return []byte("   ")
		}

		return g.RenderSpace(player, space, spaceValue, legalMoves)
	}

	for i := 0; i < 11; i++ {
		t.WriteRune(VerticalBar)
		t.Write([]byte(""))
		for j := 0; j < 12; j++ {
			t.Write(space(i, j))

			if j == 5 {
				t.WriteRune(VerticalBar)
				t.Write(space(i, -1))
				t.WriteRune(VerticalBar)
			}
		}

		t.Write([]byte("" + string(VerticalBar) + "  "))

		if i == 0 {
			t.Write([]byte(opponentColor + " " + opponentName + " (" + opponentRating + ")"))
			if g.Board[SpaceHomeOpponent] != 0 {
				v := g.Board[SpaceHomeOpponent]
				if v < 0 {
					v *= -1
				}
				t.Write([]byte(fmt.Sprintf("  %d off", v)))
			}
		} else if i == 2 {
			if g.Turn == 0 {
				if g.Player1.Name != "" && g.Player2.Name != "" {
					if opponentRoll != 0 {
						t.Write([]byte(fmt.Sprintf("  %d", opponentRoll)))
					} else {
						t.Write([]byte(fmt.Sprintf("  -")))
					}
				}
			} else if g.Turn != player {
				if g.Roll1 > 0 {
					t.Write([]byte(fmt.Sprintf("  %d  %d  ", g.Roll1, g.Roll2)))
				} else if opponentName != "" {
					t.Write([]byte(fmt.Sprintf("  -  -  ")))
				}
			}
		} else if i == 8 {
			if g.Turn == 0 {
				if g.Player1.Name != "" && g.Player2.Name != "" {
					if playerRoll != 0 {
						t.Write([]byte(fmt.Sprintf("  %d", playerRoll)))
					} else {
						t.Write([]byte(fmt.Sprintf("  -")))
					}
				}
			} else if g.Turn == player {
				if g.Roll1 > 0 {
					t.Write([]byte(fmt.Sprintf("  %d  %d  ", g.Roll1, g.Roll2)))
				} else if playerName != "" {
					t.Write([]byte(fmt.Sprintf("  -  -  ")))
				}
			}
		} else if i == 10 {
			t.Write([]byte(playerColor + " " + playerName + " (" + playerRating + ")"))
			if g.Board[SpaceHomePlayer] != 0 {
				v := g.Board[SpaceHomePlayer]
				if v < 0 {
					v *= -1
				}
				t.Write([]byte(fmt.Sprintf("  %d off", v)))
			}
		}

		t.Write([]byte(" "))
		t.WriteByte('\n')
	}

	if white {
		t.Write(boardBottomWhite)
	} else {
		t.Write(boardBottomBlack)
	}
	t.WriteString("                 \n")

	return t.Bytes()
}

func SpaceDiff(from int, to int) int {
	if from < 0 || from > 27 || to < 0 || to > 27 {
		return 0
	} else if from == SpaceHomePlayer || from == SpaceHomeOpponent || to == SpaceBarPlayer || to == SpaceBarOpponent {
		return 0
	}

	if (from == SpaceBarPlayer || from == SpaceBarOpponent) && (to == SpaceBarPlayer || to == SpaceBarOpponent || to == SpaceHomePlayer || to == SpaceHomeOpponent) {
		return 0
	}

	if from == SpaceBarPlayer {
		return 25 - to
	} else if from == SpaceBarOpponent {
		return to
	}

	if to == SpaceHomePlayer {
		return from
	} else if to == SpaceHomeOpponent {
		return 25 - from
	}

	diff := to - from
	if diff < 0 {
		return diff * -1
	}
	return diff
}

func PlayerCheckers(checkers int, player int) int {
	if player == 1 {
		if checkers > 0 {
			return checkers
		}
		return 0
	} else {
		if checkers < 0 {
			return checkers * -1
		}
		return 0
	}
}

func OpponentCheckers(checkers int, player int) int {
	if player == 2 {
		if checkers > 0 {
			return checkers
		}
		return 0
	} else {
		if checkers < 0 {
			return checkers * -1
		}
		return 0
	}
}

func FlipSpace(space int, player int) int {
	if player == 1 {
		return space
	}
	if space < 1 || space > 24 {
		switch space {
		case SpaceHomePlayer:
			return SpaceHomeOpponent
		case SpaceHomeOpponent:
			return SpaceHomePlayer
		case SpaceBarPlayer:
			return SpaceBarOpponent
		case SpaceBarOpponent:
			return SpaceBarPlayer
		default:
			return -1
		}
	}
	return 24 - space + 1
}

func FlipMoves(moves [][]int, player int) [][]int {
	m := make([][]int, len(moves))
	for i := range moves {
		m[i] = []int{FlipSpace(moves[i][0], player), FlipSpace(moves[i][1], player)}
	}
	return m
}

func FormatSpace(space int) []byte {
	if space >= 1 && space <= 24 {
		return []byte(strconv.Itoa(space))
	} else if space == SpaceBarPlayer || space == SpaceBarOpponent {
		return []byte("bar")
	} else if space == SpaceHomePlayer || space == SpaceHomeOpponent {
		return []byte("off")
	}
	return []byte("?")
}

func FormatMoves(moves [][]int) []byte {
	if len(moves) == 0 {
		return []byte("none")
	}

	var out bytes.Buffer
	for i := range moves {
		if i != 0 {
			out.WriteByte(' ')
		}
		out.Write([]byte(fmt.Sprintf("%s/%s", FormatSpace(moves[i][0]), FormatSpace(moves[i][1]))))
	}
	return out.Bytes()
}

func FormatAndFlipMoves(moves [][]int, player int) []byte {
	return FormatMoves(FlipMoves(moves, player))
}

func ValidSpace(space int) bool {
	return space >= 0 && space <= 27
}

const (
	VerticalBar rune = '\u2502' // │
)
