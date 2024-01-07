package bgammon

import (
	"bytes"
	"fmt"
	"log"
	"strconv"
	"time"
)

var boardTopBlack = []byte("+13-14-15-16-17-18-+---+19-20-21-22-23-24-+")
var boardBottomBlack = []byte("+12-11-10--9--8--7-+---+-6--5--4--3--2--1-+")

var boardTopWhite = []byte("+24-23-22-21-20-19-+---+18-17-16-15-14-13-+")
var boardBottomWhite = []byte("+-1--2--3--4--5--6-+---+-7--8--9-10-11-12-+")

const (
	VariantBackgammon int8 = 0
	VariantAceyDeucey int8 = 1
	VariantTabula     int8 = 2
)

type Game struct {
	Started time.Time
	Ended   time.Time

	Player1 Player
	Player2 Player

	Variant int8 // 0 - Backgammon, 1 - Acey-deucey, 2 - Tabula.
	Board   []int8
	Turn    int8
	Roll1   int8
	Roll2   int8
	Moves   [][]int8 // Pending moves.
	Winner  int8

	Points        int8 // Points required to win the match.
	DoubleValue   int8 // Doubling cube value.
	DoublePlayer  int8 // Player that currently posesses the doubling cube.
	DoubleOffered bool // Whether the current player is offering a double.

	Reroll bool // Used in acey-deucey.

	boardStates [][]int8 // One board state for each move to allow undoing a move.

	// Fields after this point are provided for backwards-compatibility only and will eventually be removed.
	Acey bool // For Boxcars v1.2.1 and earlier.
}

func NewGame(variant int8) *Game {
	g := &Game{
		Variant:     variant,
		Board:       NewBoard(variant),
		Player1:     NewPlayer(1),
		Player2:     NewPlayer(2),
		Points:      1,
		DoubleValue: 1,
	}
	if variant == VariantBackgammon {
		g.Player1.Entered = true
		g.Player2.Entered = true
	} else {
		// Set backwards-compatible field.
		g.Acey = true
	}
	return g
}

func (g *Game) Copy() *Game {
	newGame := &Game{
		Started: g.Started,
		Ended:   g.Ended,

		Player1: g.Player1,
		Player2: g.Player2,

		Variant: g.Variant,
		Board:   make([]int8, len(g.Board)),
		Turn:    g.Turn,
		Roll1:   g.Roll1,
		Roll2:   g.Roll2,
		Moves:   make([][]int8, len(g.Moves)),
		Winner:  g.Winner,

		Points:        g.Points,
		DoubleValue:   g.DoubleValue,
		DoublePlayer:  g.DoublePlayer,
		DoubleOffered: g.DoubleOffered,

		Reroll: g.Reroll,

		boardStates: make([][]int8, len(g.boardStates)),
	}
	copy(newGame.Board, g.Board)
	copy(newGame.Moves, g.Moves)
	copy(newGame.boardStates, g.boardStates)
	return newGame
}

func (g *Game) NextTurn(replay bool) {
	if g.Winner != 0 {
		return
	}

	// Check whether the players have finished entering the board.
	if g.Variant != VariantBackgammon {
		if !g.Player1.Entered && PlayerCheckers(g.Board[SpaceHomePlayer], 1) == 0 {
			g.Player1.Entered = true
		}
		if !g.Player2.Entered && PlayerCheckers(g.Board[SpaceHomeOpponent], 2) == 0 {
			g.Player2.Entered = true
		}
	}

	if !replay {
		var nextTurn int8 = 1
		if g.Turn == 1 {
			nextTurn = 2
		}
		g.Turn = nextTurn
	}

	g.Roll1, g.Roll2 = 0, 0
	g.Moves = g.Moves[:0]
	g.boardStates = g.boardStates[:0]
}

func (g *Game) Reset() {
	if g.Variant != VariantBackgammon {
		g.Player1.Entered = false
		g.Player2.Entered = false
	}
	g.Board = NewBoard(g.Variant)
	g.Turn = 0
	g.Roll1 = 0
	g.Roll2 = 0
	g.Moves = nil
	g.DoubleValue = 1
	g.DoublePlayer = 0
	g.DoubleOffered = false
	g.Reroll = false
	g.boardStates = nil
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

func (g *Game) addMove(move []int8) bool {
	opponentCheckers := OpponentCheckers(g.Board[move[1]], g.Turn)
	if opponentCheckers > 1 {
		return false
	}

	var delta int8 = 1
	if g.Turn == 2 {
		delta = -1
	}

	boardState := make([]int8, len(g.Board))
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

	g.Moves = append(g.Moves, []int8{move[0], move[1]})
	return true
}

// AddLocalMove adds a move without performing any validation. This is useful when
// adding a move locally while waiting for an EventBoard response from the server.
func (g *Game) AddLocalMove(move []int8) bool {
	return g.addMove(move)
}

func (g *Game) ExpandMove(move []int8, currentSpace int8, moves [][]int8, local bool) ([][]int8, bool) {
	l := g.LegalMoves(local)
	var hitMoves [][]int8
	for _, m := range l {
		if OpponentCheckers(g.Board[m[1]], g.Turn) == 1 {
			hitMoves = append(hitMoves, m)
		}
	}
	for i := 0; i < 2; i++ {
		var checkMoves [][]int8
		if i == 0 { // Try moves that will hit an opponent's checker first.
			checkMoves = hitMoves
		} else {
			checkMoves = l
		}
		for _, lm := range checkMoves {
			if lm[0] != currentSpace {
				continue
			}

			newMoves := make([][]int8, len(moves))
			copy(newMoves, moves)
			newMoves = append(newMoves, []int8{lm[0], lm[1]})

			if lm[1] == move[1] {
				return newMoves, true
			}

			currentSpace = lm[1]

			gc := g.Copy()
			gc.addMove(lm)
			m, ok := gc.ExpandMove(move, currentSpace, newMoves, local)
			if ok {
				return m, ok
			}
		}
	}
	return nil, false
}

// AddMoves adds moves to the game state.  Adding a backwards move will remove the equivalent existing move.
func (g *Game) AddMoves(moves [][]int8, local bool) (bool, [][]int8) {
	if g.Player1.Name == "" || g.Player2.Name == "" || g.Winner != 0 {
		return false, nil
	}

	var addMoves [][]int8
	var undoMoves [][]int8

	gameCopy := g.Copy()

	validateOffset := 0
VALIDATEMOVES:
	for _, move := range moves {
		l := gameCopy.LegalMoves(local)
		for _, lm := range l {
			if lm[0] == move[0] && lm[1] == move[1] {
				addMoves = append(addMoves, []int8{move[0], move[1]})
				continue VALIDATEMOVES
			}
		}

		if len(gameCopy.Moves) > 0 {
			i := len(gameCopy.Moves) - 1 - validateOffset
			if i < 0 {
				return false, nil
			}
			gameMove := gameCopy.Moves[i]
			if move[0] == gameMove[1] && move[1] == gameMove[0] {
				undoMoves = append(undoMoves, []int8{gameMove[1], gameMove[0]})
				validateOffset++
				continue VALIDATEMOVES
			}
		}

		expandedMoves, ok := g.ExpandMove(move, move[0], nil, local)
		if ok {
			for _, expanded := range expandedMoves {
				addMoves = append(addMoves, []int8{expanded[0], expanded[1]})
			}
			continue VALIDATEMOVES
		}

		return false, nil
	}

	if len(addMoves) != 0 && len(undoMoves) != 0 {
		return false, nil
	}

	var checkWin bool
ADDMOVES:
	for _, move := range addMoves {
		l := gameCopy.LegalMoves(local)
		for _, lm := range l {
			if lm[0] == move[0] && lm[1] == move[1] {
				if !gameCopy.addMove(move) {
					return false, nil
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
				return false, nil
			}
			gameMove := gameCopy.Moves[i]
			if move[0] == gameMove[1] && move[1] == gameMove[0] {
				copy(gameCopy.Board, gameCopy.boardStates[i])
				gameCopy.boardStates = gameCopy.boardStates[:i]

				gameCopy.Moves = gameCopy.Moves[:i]
				continue
			}
		}
		return false, nil
	}

	g.Board = append(g.Board[:0], gameCopy.Board...)
	g.Moves = gameCopy.Moves
	g.boardStates = gameCopy.boardStates

	if checkWin {
		entered := g.Player1.Entered
		if !local && g.Turn == 2 {
			entered = g.Player2.Entered
		}

		var foundChecker bool
		if g.Variant != VariantBackgammon && !entered {
			foundChecker = true
		} else {
			for space := 1; space <= 24; space++ {
				if PlayerCheckers(g.Board[space], g.Turn) != 0 {
					foundChecker = true
					break
				}
			}
		}

		if !foundChecker {
			g.Winner = g.Turn
		}
	}

	if len(addMoves) > 0 {
		return true, addMoves
	} else {
		return true, undoMoves
	}
}

func (g *Game) LegalMoves(local bool) [][]int8 {
	if g.Winner != 0 || g.Roll1 == 0 || g.Roll2 == 0 {
		return nil
	}

	rolls := []int8{
		g.Roll1,
		g.Roll2,
	}
	if g.Roll1 == g.Roll2 { // Rolled doubles.
		rolls = append(rolls, g.Roll1, g.Roll2)
	}

	haveDiceRoll := func(from, to int8) int8 {
		diff := SpaceDiff(from, to, g.Variant)
		var c int8
		for _, roll := range rolls {
			if roll == diff {
				c++
			}
		}
		return c
	}

	haveBearOffDiceRoll := func(diff int8) int8 {
		var c int8
		for _, roll := range rolls {
			if roll == diff || (roll > diff && g.Variant == VariantBackgammon) {
				c++
			}
		}
		return c
	}

	useDiceRoll := func(from, to int8) bool {
		if to == SpaceHomePlayer || to == SpaceHomeOpponent {
			needRoll := from
			if to == SpaceHomeOpponent {
				needRoll = 25 - from
			}
			for i, roll := range rolls {
				if roll == needRoll {
					rolls = append(rolls[:i], rolls[i+1:]...)
					return true
				}
			}
			for i, roll := range rolls {
				if roll > needRoll {
					rolls = append(rolls[:i], rolls[i+1:]...)
					return true
				}
			}
			return false
		}

		diff := SpaceDiff(from, to, g.Variant)
		for i, roll := range rolls {
			if roll == diff {
				rolls = append(rolls[:i], rolls[i+1:]...)
				return true
			}
		}
		return false
	}

	for _, move := range g.Moves {
		if !useDiceRoll(move[0], move[1]) {
			return nil
		}
	}

	var moves [][]int8
	var movesFound = make(map[int8]bool)

	var mustEnter bool
	var barSpace int8
	if PlayerCheckers(g.Board[SpaceBarPlayer], g.Turn) > 0 {
		mustEnter = true
		barSpace = SpaceBarPlayer
	} else if PlayerCheckers(g.Board[SpaceBarOpponent], g.Turn) > 0 {
		mustEnter = true
		barSpace = SpaceBarOpponent
	}
	if mustEnter { // Must enter from bar.
		from, to := HomeRange(g.opponentPlayer().Number)
		IterateSpaces(from, to, g.Variant, func(homeSpace int8, spaceCount int8) {
			if movesFound[barSpace*100+homeSpace] {
				return
			}
			available := haveDiceRoll(barSpace, homeSpace)
			if available == 0 {
				return
			}
			opponentCheckers := OpponentCheckers(g.Board[homeSpace], g.Turn)
			if opponentCheckers <= 1 {
				moves = append(moves, []int8{barSpace, homeSpace})
				movesFound[barSpace*100+homeSpace] = true
			}
		})
	} else {
		canBearOff := CanBearOff(g.Board, g.Turn, false)
		for sp := range g.Board {
			space := int8(sp)
			if space == SpaceBarPlayer || space == SpaceBarOpponent { // Handled above.
				continue
			} else if space == SpaceHomePlayer || space == SpaceHomeOpponent {
				homeSpace := SpaceHomePlayer
				entered := g.Player1.Entered
				if g.Turn == 2 {
					homeSpace = SpaceHomeOpponent
					entered = g.Player2.Entered
				}
				if g.Variant == VariantBackgammon || space != homeSpace || entered {
					continue
				}
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
				if movesFound[space*100+homeSpace] {
					continue
				}
				available := haveBearOffDiceRoll(SpaceDiff(space, homeSpace, g.Variant))
				if available > 0 {
					ok := true
					if haveDiceRoll(space, homeSpace) == 0 {
						_, homeEnd := HomeRange(g.Turn)
						if g.Turn == 2 {
							for homeSpace := space - 1; homeSpace >= homeEnd; homeSpace-- {
								if PlayerCheckers(g.Board[homeSpace], g.Turn) != 0 {
									ok = false
									break
								}
							}
						} else {
							for homeSpace := space + 1; homeSpace <= homeEnd; homeSpace++ {
								if PlayerCheckers(g.Board[homeSpace], g.Turn) != 0 {
									ok = false
									break
								}
							}
						}
					}
					if ok {
						moves = append(moves, []int8{space, homeSpace})
						movesFound[space*100+homeSpace] = true
					}
				}
			}

			// Move normally.
			var lastSpace int8 = 1
			if g.Turn == 2 {
				lastSpace = 24
			}

			f := func(to int8, spaceCount int8) {
				if movesFound[space*100+to] {
					return
				}
				available := haveDiceRoll(space, to)
				if available == 0 {
					return
				}

				opponentCheckers := OpponentCheckers(g.Board[to], g.Turn)
				if opponentCheckers <= 1 {
					moves = append(moves, []int8{space, to})
					movesFound[space*100+to] = true
				}
			}
			if space == SpaceHomePlayer {
				IterateSpaces(25, lastSpace, g.Variant, f)
			} else if space == SpaceHomeOpponent {
				IterateSpaces(1, lastSpace, g.Variant, f)
			} else {
				IterateSpaces(space, lastSpace, g.Variant, f)
			}
		}
	}

	// totalMoves tries all legal moves in a game and returns the maximum total number of moves that a player may consecutively make.
	var totalMoves func(in *Game, move []int8) int8
	totalMoves = func(in *Game, move []int8) int8 {
		gc := in.Copy()
		if !gc.addMove(move) {
			log.Panicf("failed to add move %+v to game %+v", move, in)
		}

		var maxTotal int8 = 1
		for _, m := range gc.LegalMoves(local) {
			total := totalMoves(gc, m)
			if total+1 > maxTotal {
				maxTotal = total + 1
			}
		}
		return maxTotal
	}

	// Simulate all possible moves to their final value and only allow moves that will achieve the maximum total moves.
	var maxMoves int8
	moveCounts := make([]int8, len(moves))
	for i, move := range moves {
		moveCounts[i] = totalMoves(g, move)
		if moveCounts[i] > maxMoves {
			maxMoves = moveCounts[i]
		}
	}
	if maxMoves > 1 {
		var newMoves [][]int8
		for i, move := range moves {
			if moveCounts[i] >= maxMoves {
				newMoves = append(newMoves, move)
			}
		}
		moves = newMoves
	}

	return moves
}

func (g *Game) RenderSpace(player int8, space int8, spaceValue int8, legalMoves [][]int8) []byte {
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

	var firstDigit int8 = 4
	var secondDigit int8 = 5
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
			firstNumeral = strconv.Itoa(int(abs))
		}
		if abs > 9 {
			secondNumeral = strconv.Itoa(int(abs) - 10)
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

func (g *Game) BoardState(player int8, local bool) []byte {
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

	legalMoves := g.LegalMoves(local)
	space := func(row int8, col int8) []byte {
		var spaceValue int8 = row + 1
		if row > 5 {
			spaceValue = 5 - (row - 6)
		}

		if col == -1 {
			if row <= 4 {
				return g.RenderSpace(player, SpaceBarOpponent, spaceValue, legalMoves)
			}
			return g.RenderSpace(player, SpaceBarPlayer, spaceValue, legalMoves)
		}

		var space int8
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

	const verticalBar rune = '│'
	for i := int8(0); i < 11; i++ {
		t.WriteRune(verticalBar)
		t.Write([]byte(""))
		for j := int8(0); j < 12; j++ {
			t.Write(space(i, j))

			if j == 5 {
				t.WriteRune(verticalBar)
				t.Write(space(i, -1))
				t.WriteRune(verticalBar)
			}
		}

		t.Write([]byte("" + string(verticalBar) + "  "))

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
						t.Write([]byte("  -"))
					}
				}
			} else if g.Turn != player {
				if g.Roll1 > 0 {
					t.Write([]byte(fmt.Sprintf("  %d  %d  ", g.Roll1, g.Roll2)))
				} else if opponentName != "" {
					t.Write([]byte("  -  -  "))
				}
			}
		} else if i == 8 {
			if g.Turn == 0 {
				if g.Player1.Name != "" && g.Player2.Name != "" {
					if playerRoll != 0 {
						t.Write([]byte(fmt.Sprintf("  %d", playerRoll)))
					} else {
						t.Write([]byte("  -"))
					}
				}
			} else if g.Turn == player {
				if g.Roll1 > 0 {
					t.Write([]byte(fmt.Sprintf("  %d  %d  ", g.Roll1, g.Roll2)))
				} else if playerName != "" {
					t.Write([]byte("  -  -  "))
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

func SpaceDiff(from int8, to int8, variant int8) int8 {
	if from < 0 || from > 27 || to < 0 || to > 27 {
		return 0
	} else if to == SpaceBarPlayer || to == SpaceBarOpponent {
		return 0
	} else if from == SpaceHomePlayer || from == SpaceHomeOpponent {
		if variant != VariantBackgammon {
			if from == SpaceHomePlayer {
				return 25 - to
			} else {
				return to
			}
		}
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

func IterateSpaces(from int8, to int8, variant int8, f func(space int8, spaceCount int8)) {
	if from == to || from < 0 || from > 25 || to < 0 || to > 25 {
		return
	} else if variant == VariantBackgammon && (from == 0 || from == 25 || to == 0 || to == 25) {
		return
	}
	var i int8 = 1
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

func PlayerCheckers(checkers int8, player int8) int8 {
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

func OpponentCheckers(checkers int8, player int8) int8 {
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

func FlipSpace(space int8, player int8) int8 {
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

func FlipMoves(moves [][]int8, player int8) [][]int8 {
	m := make([][]int8, len(moves))
	for i := range moves {
		m[i] = []int8{FlipSpace(moves[i][0], player), FlipSpace(moves[i][1], player)}
	}
	return m
}

func FormatSpace(space int8) []byte {
	if space >= 1 && space <= 24 {
		return []byte(strconv.Itoa(int(space)))
	} else if space == SpaceBarPlayer || space == SpaceBarOpponent {
		return []byte("bar")
	} else if space == SpaceHomePlayer || space == SpaceHomeOpponent {
		return []byte("off")
	}
	return []byte("?")
}

func FormatMoves(moves [][]int8) []byte {
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

func FormatAndFlipMoves(moves [][]int8, player int8) []byte {
	return FormatMoves(FlipMoves(moves, player))
}

func ValidSpace(space int8) bool {
	return space >= 0 && space <= 27
}
