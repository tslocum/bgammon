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
		Board:   make([]int, len(g.Board)),
		Player1: g.Player1,
		Player2: g.Player2,
		Turn:    g.Turn,
		Roll1:   g.Roll1,
		Roll2:   g.Roll2,
		Moves:   make([][]int, len(g.Moves)),
	}
	copy(newGame.Board, g.Board)
	copy(newGame.Moves, g.Moves)
	return newGame
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
	if from == to {
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

// AddMoves adds moves to the game state.  Adding a backwards move will remove the equivalent existing move.
func (g *Game) AddMoves(moves [][]int) bool {
	log.Printf("ADD MOVES - %+v", moves)

	delta := 1
	if g.Turn == 2 {
		delta = -1
	}

	var addMoves [][]int
	var undoMoves [][]int

	newBoard := make([]int, len(g.Board))
	var boardStates [][]int
	copy(newBoard, g.Board)
	validateOffset := 0
VALIDATEMOVES:
	for _, move := range moves {
		l := g.LegalMoves()
		for _, lm := range l {
			if lm[0] == move[0] && lm[1] == move[1] {
				addMoves = append(addMoves, []int{move[0], move[1]})
				continue VALIDATEMOVES
			}
		}
		if len(g.Moves) > 0 {
			i := len(g.Moves) - 1 - validateOffset
			if i < 0 {
				log.Printf("FAILED MOVE %d/%d", move[0], move[1])
				return false
			}
			gameMove := g.Moves[i]
			if move[0] == gameMove[1] && move[1] == gameMove[0] {
				undoMoves = append(undoMoves, []int{gameMove[1], gameMove[0]})
				validateOffset++
				continue VALIDATEMOVES
			}
		}
		log.Printf("FAILED MOVE %d/%d", move[0], move[1])
		return false
	}

	if len(addMoves) != 0 && len(undoMoves) != 0 {
		return false
	}

ADDMOVES:
	for _, move := range addMoves {
		l := g.LegalMoves()
		for _, lm := range l {
			if lm[0] == move[0] && lm[1] == move[1] {
				log.Printf("ADD MOV %d/%d", lm[0], lm[1])

				boardState := make([]int, len(newBoard))
				copy(boardState, newBoard)
				boardStates = append(boardStates, boardState)

				newBoard[move[0]] -= delta
				opponentCheckers := OpponentCheckers(newBoard[lm[1]], g.Turn)
				if opponentCheckers == 1 {
					newBoard[move[1]] = delta
				} else {
					newBoard[move[1]] += delta
				}
				continue ADDMOVES
			}
		}
	}
	if len(addMoves) != 0 {
		g.Moves = append(g.Moves, moves...)
		g.boardStates = append(g.boardStates, boardStates...)
	}

	newMoves := make([][]int, len(g.Moves))
	copy(newMoves, g.Moves)
	newBoardStates := make([][]int, len(g.boardStates))
	copy(newBoardStates, g.boardStates)
	for _, move := range undoMoves {
		log.Printf("TRY UNDO MOV %d/%d %+v", move[0], move[1], newMoves)
		if len(g.Moves) > 0 {
			i := len(newMoves) - 1
			if i < 0 {
				log.Printf("FAILED UNDO MOVE %d/%d", move[0], move[1])
				return false
			}
			gameMove := newMoves[i]
			if move[0] == gameMove[1] && move[1] == gameMove[0] {
				log.Printf("UNDO MOV %d/%d", gameMove[0], gameMove[1])

				copy(newBoard, g.boardStates[i])
				newMoves = g.Moves[:i]
				newBoardStates = g.boardStates[:i]
				log.Printf("NEW MOVES %+v", newMoves)
				continue
			}
			log.Printf("COMPARE MOV %d/%d %d/%d", gameMove[0], gameMove[1], move[0], move[1])
		}
		log.Printf("FAILED UNDO MOVE %d/%d", move[0], move[1])
		return false
	}
	if len(undoMoves) != 0 {
		g.Moves = newMoves
		g.boardStates = newBoardStates
	}

	g.Board = newBoard
	return true
}

func (g *Game) LegalMoves() [][]int {
	if g.Roll1 == 0 || g.Roll2 == 0 {
		return nil
	}

	rolls := []int{
		g.Roll1,
		g.Roll2,
	}
	if g.Roll1 == g.Roll2 { // Rolled doubles.
		rolls = append(rolls, g.Roll1, g.Roll2)
	}

	haveDiceRoll := func(from, to int) bool {
		// TODO diff needs to account for bar and home special spaces
		diff := to - from
		if diff < 0 {
			diff *= -1
		}
		for _, roll := range rolls {
			if roll == diff {
				return true
			}
		}
		return false
	}

	useDiceRoll := func(from, to int) {
		// TODO diff needs to account for bar and home special spaces
		diff := to - from
		if diff < 0 {
			diff *= -1
		}
		for i, roll := range rolls {
			if roll == diff {
				rolls = append(rolls[:i], rolls[i+1:]...)
				return
			}
		}
		log.Panicf("tried to use non-existent dice roll %d-%d, have %+v", from, to, rolls)
	}

	for _, move := range g.Moves {
		useDiceRoll(move[0], move[1])
	}

	var moves [][]int
	for space := range g.Board {
		if space == SpaceHomePlayer || space == SpaceHomeOpponent {
			continue
		}

		checkers := g.Board[space]
		playerCheckers := PlayerCheckers(checkers, g.Turn)
		if playerCheckers == 0 {
			continue
		}

		if space == SpaceBarPlayer || space == SpaceBarOpponent {
			// Enter from bar.
			from, to := HomeRange(g.Turn)
			g.iterateSpaces(from, to, func(homeSpace int, spaceCount int) {
				if !haveDiceRoll(space, homeSpace) {
					return
				}
				if spaceCount != g.Roll1 && spaceCount != g.Roll2 {
					return
				}
				opponentCheckers := OpponentCheckers(g.Board[homeSpace], g.Turn)
				if opponentCheckers <= 1 {
					moves = append(moves, []int{space, homeSpace})
				}
			})
		} else {
			// Move normally.
			lastSpace := 1
			dir := -1
			if g.Turn == 2 {
				lastSpace = 24
				dir = 1
			}

			if space == lastSpace {
				continue // TODO check if all pieces in home
			}

			g.iterateSpaces(space+dir, lastSpace, func(to int, spaceCount int) {
				if !haveDiceRoll(space, to) {
					return
				}

				if to == SpaceHomePlayer || to == SpaceHomeOpponent {
					return // TODO
				}

				opponentCheckers := OpponentCheckers(g.Board[to], g.Turn)
				if opponentCheckers <= 1 {
					movable := 1
					if g.Roll1 == g.Roll2 {
						movable = playerCheckers
						if movable > 4 {
							movable = 4
						}
					}
					for i := 0; i < movable; i++ {
						moves = append(moves, []int{space, to})
						//log.Printf("ADD MOVE %d-%d", space, to)
					}
				}
			})
		}
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

func spaceDiff(from int, to int) int {
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
			log.Panicf("unknown space %d", space)
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

func FormatMoves(moves [][]int, player int) []byte {
	var out bytes.Buffer
	for i := range moves {
		if i != 0 {
			out.WriteByte(' ')
		}
		// TODO
		out.Write([]byte(fmt.Sprintf("{{%d/%d}}", FlipSpace(moves[i][0], player), FlipSpace(moves[i][1], player))))
	}
	return out.Bytes()
}

const (
	VerticalBar rune = '\u2502' // │
)
