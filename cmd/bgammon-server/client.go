package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

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
	terminating  bool
	bgammon.Client
}

func (c *serverClient) sendEvent(e interface{}) {
	// JSON formatted messages.
	if c.json {
		switch ev := e.(type) {
		case *bgammon.EventWelcome:
			ev.Type = bgammon.EventTypeWelcome
		case *bgammon.EventHelp:
			ev.Type = bgammon.EventTypeHelp
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
		case *bgammon.EventLeft:
			ev.Type = bgammon.EventTypeLeft
		case *bgammon.EventBoard:
			ev.Type = bgammon.EventTypeBoard
		case *bgammon.EventRolled:
			ev.Type = bgammon.EventTypeRolled
		case *bgammon.EventFailedRoll:
			ev.Type = bgammon.EventTypeFailedRoll
		case *bgammon.EventMoved:
			ev.Type = bgammon.EventTypeMoved
		case *bgammon.EventFailedMove:
			ev.Type = bgammon.EventTypeFailedMove
		case *bgammon.EventFailedOk:
			ev.Type = bgammon.EventTypeFailedOk
		case *bgammon.EventWin:
			ev.Type = bgammon.EventTypeWin
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

	// Human-readable messages.
	switch ev := e.(type) {
	case *bgammon.EventWelcome:
		c.Write([]byte(fmt.Sprintf("welcome %s there are %d clients playing %d matches.", ev.PlayerName, ev.Clients, ev.Games)))
	case *bgammon.EventHelp:
		c.Write([]byte("helpstart Help text:"))
		c.Write([]byte(fmt.Sprintf("help %s", ev.Message)))
		c.Write([]byte("helpend End of help text."))
	case *bgammon.EventPing:
		c.Write([]byte(fmt.Sprintf("ping %s", ev.Message)))
	case *bgammon.EventNotice:
		c.Write([]byte(fmt.Sprintf("notice %s", ev.Message)))
	case *bgammon.EventSay:
		c.Write([]byte(fmt.Sprintf("say %s %s", ev.Player, ev.Message)))
	case *bgammon.EventList:
		c.Write([]byte("liststart Matches list:"))
		for _, g := range ev.Games {
			password := 0
			if g.Password {
				password = 1
			}
			name := "(No name)"
			if g.Name != "" {
				name = g.Name
			}
			c.Write([]byte(fmt.Sprintf("game %d %d %d %s", g.ID, password, g.Players, name)))
		}
		c.Write([]byte("listend End of matches list."))
	case *bgammon.EventJoined:
		c.Write([]byte(fmt.Sprintf("joined %d %d %s", ev.GameID, ev.PlayerNumber, ev.Player)))
	case *bgammon.EventFailedJoin:
		c.Write([]byte(fmt.Sprintf("failedjoin %s", ev.Reason)))
	case *bgammon.EventLeft:
		c.Write([]byte(fmt.Sprintf("left %s", ev.Player)))
	case *bgammon.EventRolled:
		c.Write([]byte(fmt.Sprintf("rolled %s %d %d", ev.Player, ev.Roll1, ev.Roll2)))
	case *bgammon.EventFailedRoll:
		c.Write([]byte(fmt.Sprintf("failedroll %s", ev.Reason)))
	case *bgammon.EventMoved:
		c.Write([]byte(fmt.Sprintf("moved %s %s", ev.Player, bgammon.FormatAndFlipMoves(ev.Moves, c.playerNumber))))
	case *bgammon.EventFailedMove:
		c.Write([]byte(fmt.Sprintf("failedmove %d/%d %s", ev.From, ev.To, ev.Reason)))
	case *bgammon.EventFailedOk:
		c.Write([]byte(fmt.Sprintf("failedok %s", ev.Reason)))
	case *bgammon.EventWin:
		c.Write([]byte(fmt.Sprintf("win %s wins!", ev.Player)))
	default:
		log.Panicf("unknown event type %+v", ev)
	}
}

func (c *serverClient) sendNotice(message string) {
	c.sendEvent(&bgammon.EventNotice{
		Message: message,
	})
}

func (c *serverClient) label() string {
	if len(c.name) > 0 {
		return string(c.name)
	}
	return strconv.Itoa(c.id)
}

func (c *serverClient) Terminate(reason string) {
	if c.Terminated() || c.terminating {
		return
	}
	c.terminating = true

	var extra string
	if reason != "" {
		extra = ": " + reason
	}

	go func() {
		c.sendNotice("Connection terminated" + extra)

		time.Sleep(time.Second)
		c.Client.Terminate(reason)
	}()
}

func logClientRead(msg []byte) {
	msgLower := bytes.ToLower(msg)
	if bytes.HasPrefix(msgLower, []byte("login ")) || bytes.HasPrefix(msgLower, []byte("l ")) || bytes.HasPrefix(msgLower, []byte("loginjson ")) || bytes.HasPrefix(msgLower, []byte("lj ")) {
		split := bytes.Split(msg, []byte(" "))
		var username []byte
		var password []byte
		l := len(split)
		if l > 1 {
			username = split[1]
			if l > 2 {
				password = []byte("*******")
			}
		}
		log.Printf("<- %s %s %s", split[0], username, password)
	} else {
		log.Printf("<- %s", msg)
	}
}
