package main

import (
	"encoding/json"
	"fmt"
	"log"

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
	playerNumber int
	bgammon.Client
}

func (c *serverClient) sendEvent(e interface{}) {
	if c.json {
		switch ev := e.(type) {
		case *bgammon.EventWelcome:
			ev.Type = bgammon.EventTypeWelcome
		case *bgammon.EventPing:
			ev.Type = bgammon.EventTypePing
		case *bgammon.EventNotice:
			ev.Type = bgammon.EventTypeNotice
		case *bgammon.EventSay:
			ev.Type = bgammon.EventTypeSay
		case *bgammon.EventList:
			ev.Type = bgammon.EventTypeList
		case *bgammon.EventJoined:
			ev.Type = bgammon.EventTypeJoined
		case *bgammon.EventFailedJoin:
			ev.Type = bgammon.EventTypeFailedJoin
		case *bgammon.EventBoard:
			ev.Type = bgammon.EventTypeBoard
		case *bgammon.EventRolled:
			ev.Type = bgammon.EventTypeRolled
		case *bgammon.EventMoved:
			ev.Type = bgammon.EventTypeMoved
		default:
			log.Panicf("unknown event type %+v", ev)
		}

		buf, err := json.Marshal(e)
		if err != nil {
			panic(err)
		}
		c.Write(buf)
		return
	}

	switch ev := e.(type) {
	case *bgammon.EventWelcome:
		c.Write([]byte(fmt.Sprintf("welcome %s there are %d clients playing %d games.", ev.PlayerName, ev.Clients, ev.Games)))
	case *bgammon.EventPing:
		c.Write([]byte(fmt.Sprintf("ping %s", ev.Message)))
	case *bgammon.EventNotice:
		c.Write([]byte(fmt.Sprintf("notice %s", ev.Message)))
	case *bgammon.EventSay:
		c.Write([]byte(fmt.Sprintf("say %s %s", ev.Player, ev.Message)))
	case *bgammon.EventList:
		c.Write([]byte("liststart Games list:"))
		for _, g := range ev.Games {
			password := 0
			if g.Password {
				password = 1
			}
			name := "Game"
			if g.Name != "" {
				name = g.Name
			}
			c.Write([]byte(fmt.Sprintf("game %d %d %d %s", g.ID, password, g.Players, name)))
		}
		c.Write([]byte("listend End of games list."))
	case *bgammon.EventJoined:
		c.Write([]byte(fmt.Sprintf("joined %d %s", ev.GameID, ev.Player)))
	case *bgammon.EventFailedJoin:
		c.Write([]byte(fmt.Sprintf("failedjoin %s", ev.Reason)))
	case *bgammon.EventRolled:
		c.Write([]byte(fmt.Sprintf("rolled %s %d %d", ev.Player, ev.Roll1, ev.Roll2)))
	case *bgammon.EventMoved:
		c.Write([]byte(fmt.Sprintf("moved %s %s", ev.Player, bgammon.FormatMoves(ev.Moves, c.playerNumber))))
	default:
		log.Panicf("unknown event type %+v", ev)
	}
}

func (c *serverClient) sendNotice(message string) {
	c.sendEvent(&bgammon.EventNotice{
		Message: message,
	})
}
