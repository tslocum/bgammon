package main

import (
	"encoding/json"
	"fmt"

	"code.rocket9labs.com/tslocum/bgammon"
)

type serverClient struct {
	id           int
	json         bool
	name         []byte
	account      int
	connected    int64
	lastActive   int64
	lastPing     int64
	commands     <-chan []byte
	events       chan<- []byte
	playerNumber int
	bgammon.Client
}

func (c *serverClient) sendEvent(e interface{}) {
	if c.json {
		switch ev := e.(type) {
		case *bgammon.EventWelcome:
			ev.Type = "welcome"
		case *bgammon.EventNotice:
			ev.Type = "notice"
		case *bgammon.EventSay:
			ev.Type = "say"
		case *bgammon.EventList:
			ev.Type = "list"
		case *bgammon.EventJoined:
			ev.Type = "joined"
		case *bgammon.EventFailedJoin:
			ev.Type = "failedjoin"
		case *bgammon.EventBoard:
			ev.Type = "board"
		case *bgammon.EventRolled:
			ev.Type = "rolled"
		case *bgammon.EventMoved:
			ev.Type = "moved"
		}

		buf, err := json.Marshal(e)
		if err != nil {
			panic(err)
		}
		c.events <- buf
		return
	}

	switch ev := e.(type) {
	case *bgammon.EventWelcome:
		c.events <- []byte(fmt.Sprintf("welcome %s there are %d clients playing %d games.", ev.PlayerName, ev.Clients, ev.Games))
	case *bgammon.EventNotice:
		c.events <- []byte(fmt.Sprintf("notice %s", ev.Message))
	case *bgammon.EventSay:
		c.events <- []byte(fmt.Sprintf("say %s %s", ev.Player, ev.Message))
	case *bgammon.EventList:
		c.events <- []byte("liststart Games list:")
		for _, g := range ev.Games {
			password := 0
			if g.Password {
				password = 1
			}
			name := "Game"
			if g.Name != "" {
				name = g.Name
			}
			c.events <- []byte(fmt.Sprintf("game %d %d %d %s", g.ID, password, g.Players, name))
		}
		c.events <- []byte("listend End of games list.")
	case *bgammon.EventJoined:
		c.events <- []byte(fmt.Sprintf("joined %d %s", ev.GameID, ev.Player))
	case *bgammon.EventFailedJoin:
		c.events <- []byte(fmt.Sprintf("failedjoin %s", ev.Reason))
	case *bgammon.EventRolled:
		c.events <- []byte(fmt.Sprintf("rolled %s %d %d", ev.Player, ev.Roll1, ev.Roll2))
	case *bgammon.EventMoved:
		c.events <- []byte(fmt.Sprintf("moved %s %s", ev.Player, bgammon.FormatMoves(ev.Moves, c.playerNumber)))
	}
}

func (c *serverClient) sendNotice(message string) {
	c.sendEvent(&bgammon.EventNotice{
		Message: message,
	})
}
