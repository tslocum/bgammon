package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"slices"
	"time"

	"code.rocket9labs.com/tslocum/bgammon"
)

type serverGame struct {
	id         int
	created    int64
	lastActive int64
	name       []byte
	password   []byte
	client1    *serverClient
	client2    *serverClient
	r          *rand.Rand
	*bgammon.Game
}

func newServerGame(id int) *serverGame {
	now := time.Now().Unix()
	return &serverGame{
		id:         id,
		created:    now,
		lastActive: now,
		Game:       bgammon.NewGame(),
		r:          rand.New(rand.NewSource(time.Now().UnixNano() + rand.Int63n(1000000))),
	}
}

func (g *serverGame) roll(player int) bool {
	if g.Turn == 0 {
		if player == 1 {
			if g.Roll1 != 0 {
				return false
			}
			g.Roll1 = g.r.Intn(6) + 1
			return true
		} else {
			if g.Roll2 != 0 {
				return false
			}
			g.Roll2 = g.r.Intn(6) + 1
			return true
		}
	} else if player != g.Turn || g.Roll1 != 0 || g.Roll2 != 0 {
		return false
	}
	g.Roll1, g.Roll2 = g.r.Intn(6)+1, g.r.Intn(6)+1
	return true
}

func (g *serverGame) sendBoard(client *serverClient) {
	playerNumber := 1
	if g.client2 == client {
		playerNumber = 2
	}

	if client.json {
		gameState := ServerGameState{
			GameState: bgammon.GameState{
				Game:      g.Game,
				Available: g.LegalMoves(),
			},
			Board: g.Game.Board,
		}
		if playerNumber == 2 {
			log.Println(gameState.Board)
			log.Println(g.Game.Board)
			slices.Reverse(gameState.Board)

			log.Println(gameState.Board)
			log.Println(g.Game.Board)
		}
		buf, err := json.Marshal(gameState)
		if err != nil {
			log.Fatalf("failed to marshal json for %+v: %s", gameState, err)
		}
		client.events <- []byte(fmt.Sprintf("board %s", buf))
		return
	}

	scanner := bufio.NewScanner(bytes.NewReader(g.BoardState(playerNumber)))
	for scanner.Scan() {
		client.events <- append([]byte("notice "), scanner.Bytes()...)
	}
}

func (g *serverGame) playerCount() int {
	c := 0
	if g.client1 != nil {
		c++
	}
	if g.client2 != nil {
		c++
	}
	return c
}

func (g *serverGame) eachClient(f func(client *serverClient)) {
	if g.client1 != nil {
		f(g.client1)
	}
	if g.client2 != nil {
		f(g.client2)
	}
}

func (g *serverGame) addClient(client *serverClient) bool {
	var ok bool
	defer func() {
		if !ok {
			return
		}
		joinMessage := []byte(fmt.Sprintf("joined %d %s %s", g.id, client.name, g.name))
		client.events <- joinMessage
		g.sendBoard(client)
		opponent := g.opponent(client)
		if opponent != nil {
			opponent.events <- joinMessage
			g.sendBoard(opponent)
		}
	}()
	switch {
	case g.client1 != nil && g.client2 != nil:
		ok = false
	case g.client1 != nil:
		g.client2 = client
		g.Player2.Name = string(client.name)
		ok = true
	case g.client2 != nil:
		g.client1 = client
		g.Player1.Name = string(client.name)
		ok = true
	default:
		i := rand.Intn(2)
		if i == 0 {
			g.client1 = client
			g.Player1.Name = string(client.name)
		} else {
			g.client2 = client
			g.Player2.Name = string(client.name)
		}
		ok = true
	}
	return ok
}

func (g *serverGame) removeClient(client *serverClient) {
	// TODO game is considered paused when only one player is present
	// once started, only the same player may join and continue the game
	log.Println("remove client", client)
	ok := true
	defer func() {
		if !ok {
			return
		}
		opponent := g.opponent(client)
		if opponent == nil {
			return
		}
		opponent.events <- []byte(fmt.Sprintf("left %d %s %s", g.id, client.name, g.name))
		if !opponent.json {
			g.sendBoard(opponent)
		}
	}()
	switch {
	case g.client1 == client:
		g.client1 = nil
		g.Player1.Name = ""
	case g.client2 == client:
		g.client2 = nil
		g.Player2.Name = ""
	default:
		ok = false
		return
	}
}

func (g *serverGame) opponent(client *serverClient) *serverClient {
	if g.client1 == client {
		return g.client2
	} else if g.client2 == client {
		return g.client1
	}
	return nil
}

type ServerGameState struct {
	bgammon.GameState
	Board []int
}
