package bgammon

import (
	"bytes"
	"fmt"
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
}

func NewGame() *Game {
	return &Game{
		Board:   NewBoard(),
		Player1: NewPlayer(1),
		Player2: NewPlayer(2),
	}
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

func (g *Game) LegalMoves() [][]int {
	if g.Roll1 == 0 || g.Roll2 == 0 {
		return nil
	}

	var moves [][]int
	for space := range g.Board {
		if space == SpaceHomePlayer || space == SpaceHomeOpponent {
			continue
		}

		checkers := g.Board[space]
		playerCheckers := numPlayerCheckers(checkers, g.Turn)
		if playerCheckers == 0 {
			continue
		}

		if space == SpaceBarPlayer || space == SpaceBarOpponent {
			// Enter from bar.
			from, to := HomeRange(g.Turn)
			g.iterateSpaces(from, to, func(homeSpace int, spaceCount int) {
				if spaceCount != g.Roll1 && spaceCount != g.Roll2 {
					return
				}
				opponentCheckers := numOpponentCheckers(g.Board[homeSpace], g.Turn)
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
				if spaceCount != g.Roll1 && spaceCount != g.Roll2 {
					return
				}

				if to == SpaceHomePlayer || to == SpaceHomeOpponent {
					return // TODO
				}

				opponentCheckers := numOpponentCheckers(g.Board[to], g.Turn)
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
			pieceColor = "x"
		} else if value > 0 {
			pieceColor = "o"
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
	if white {
		playerColor = "o"
		opponentColor = "x"
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

		var index int
		if !white {
			if row < 6 {
				col = 12 - col
			} else {
				col = 11 - col
			}

			index = col
			if row > 5 {
				index = 11 - col + 13
			}
		} else {
			index = col + 3
			if row > 5 {
				index = 11 - col + 15
			}
		}
		if white {
			index = BoardSpaces - 1 - index
		}

		if row == 5 {
			return []byte("   ")
		}

		return g.RenderSpace(player, index, spaceValue, legalMoves)
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
			if g.Turn != player && g.Roll1 > 0 {
				t.Write([]byte(fmt.Sprintf("  %d  %d  ", g.Roll1, g.Roll2)))
			} else {
				t.Write([]byte(fmt.Sprintf("  -  -  ")))
			}
		} else if i == 8 {
			if g.Turn == player && g.Roll1 > 0 {
				t.Write([]byte(fmt.Sprintf("  %d  %d  ", g.Roll1, g.Roll2)))
			} else {
				t.Write([]byte(fmt.Sprintf("  -  -  ")))
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

func numPlayerCheckers(checkers int, player int) int {
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

func numOpponentCheckers(checkers int, player int) int {
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

const (
	VerticalBar rune = '\u2502' // │
)
