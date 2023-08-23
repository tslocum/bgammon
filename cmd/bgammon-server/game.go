package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
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
	if client.json {
		buf, err := json.Marshal(g.Game)
		if err != nil {
			log.Fatalf("failed to marshal json for %+v: %s", g.Game, err)
		}
		client.events <- []byte(fmt.Sprintf("board %s", buf))
		return
	}

	playerNumber := 1
	if g.client2 == client {
		playerNumber = 2
	}
	scanner := bufio.NewScanner(bytes.NewReader(g.BoardState(playerNumber)))
	for scanner.Scan() {
		client.events <- append([]byte("notice "), scanner.Bytes()...)
	}
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
		opponent := g.opponent(client)
		if opponent != nil {
			opponent.events <- joinMessage
		}
	}()
	switch {
	case g.client1 != nil && g.client2 != nil:
		ok = false
	case g.client1 != nil:
		g.client2 = client
		ok = true
	case g.client2 != nil:
		g.client1 = client
		ok = true
	default:
		i := rand.Intn(2)
		if i == 0 {
			g.client1 = client
		} else {
			g.client2 = client
		}
		ok = true
	}
	return ok
}

func (g *serverGame) opponent(client *serverClient) *serverClient {
	if g.client1 == client {
		return g.client2
	} else if g.client2 == client {
		return g.client1
	}
	return nil
}
