package main

import (
	"bufio"
	"bytes"
	"log"
	"time"

	"code.rocket9labs.com/tslocum/bgammon"
)

func main() {
	b := bgammon.NewBoard()
	log.Printf("%+v", b)

	s := newServer()
	go s.listen("tcp", "127.0.0.1:1337")

	g := newServerGame(1)
	g.Board[bgammon.SpaceBarPlayer] = 3
	g.Board[bgammon.SpaceBarOpponent] = -2
	g.Roll1 = 1
	g.Roll2 = 3
	g.Turn = 2
	log.Println("initial legal moves")
	log.Printf("%+v", g.LegalMoves())

	//g.Moves = append(g.Moves, []int{6, 4})
	log.Printf("Legal moves after %+v", g.Moves)
	log.Printf("%+v", g.LegalMoves())

	playerNumber := 2

	go func() {
		time.Sleep(100 * time.Millisecond)
		scanner := bufio.NewScanner(bytes.NewReader(g.BoardState(playerNumber)))
		for scanner.Scan() {
			log.Printf("%s", append([]byte("notice "), scanner.Bytes()...))
		}

	}()
	select {}
}
