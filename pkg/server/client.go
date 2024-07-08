package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"code.rocket9labs.com/tslocum/bgammon"
	"github.com/leonelquinteros/gotext"
)

type clientRating struct {
	backgammonSingle int
	backgammonMulti  int
	aceySingle       int
	aceyMulti        int
	tabulaSingle     int
	tabulaMulti      int
}

func (r *clientRating) getRating(variant int8, multiPoint bool) int {
	switch variant {
	case bgammon.VariantBackgammon:
		if !multiPoint {
			return r.backgammonSingle
		}
		return r.backgammonMulti
	case bgammon.VariantAceyDeucey:
		if !multiPoint {
			return r.aceySingle
		}
		return r.aceyMulti
	case bgammon.VariantTabula:
		if !multiPoint {
			return r.tabulaSingle
		}
		return r.tabulaMulti
	default:
		log.Panicf("unknown variant: %d", variant)
		return 0
	}
}

func (r *clientRating) setRating(variant int8, multiPoint bool, rating int) {
	switch variant {
	case bgammon.VariantBackgammon:
		if !multiPoint {
			r.backgammonSingle = rating
			return
		}
		r.backgammonMulti = rating
	case bgammon.VariantAceyDeucey:
		if !multiPoint {
			r.aceySingle = rating
			return
		}
		r.aceyMulti = rating
	case bgammon.VariantTabula:
		if !multiPoint {
			r.tabulaSingle = rating
		}
		r.tabulaMulti = rating
	default:
		log.Panicf("unknown variant: %d", variant)
	}
}

type serverClient struct {
	id           int
	json         bool
	name         []byte
	language     string
	account      *account
	accountID    int
	connected    int64
	active       int64
	lastPing     int64
	commands     chan []byte
	autoplay     bool
	playerNumber int8
	terminating  bool
	bgammon.Client
}

func (c *serverClient) Admin() bool {
	return c.accountID == 1
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
		case *bgammon.EventFailedLeave:
			ev.Type = bgammon.EventTypeFailedLeave
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
		case *bgammon.EventSettings:
			ev.Type = bgammon.EventTypeSettings
		case *bgammon.EventReplay:
			ev.Type = bgammon.EventTypeReplay
		case *bgammon.EventHistory:
			ev.Type = bgammon.EventTypeHistory
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
			c.Write([]byte(fmt.Sprintf("game %d %d %d %d %s", g.ID, password, g.Points, g.Players, name)))
		}
		c.Write([]byte("listend End of matches list."))
	case *bgammon.EventJoined:
		c.Write([]byte(fmt.Sprintf("joined %d %d %s", ev.GameID, ev.PlayerNumber, ev.Player)))
	case *bgammon.EventFailedJoin:
		c.Write([]byte(fmt.Sprintf("failedjoin %s", ev.Reason)))
	case *bgammon.EventLeft:
		c.Write([]byte(fmt.Sprintf("left %s", ev.Player)))
	case *bgammon.EventRolled:
		msg := []byte(fmt.Sprintf("rolled %s %d %d", ev.Player, ev.Roll1, ev.Roll2))
		if ev.Roll3 != 0 {
			msg = append(msg, []byte(fmt.Sprintf(" %d", ev.Roll3))...)
		}
		c.Write(msg)
	case *bgammon.EventFailedRoll:
		c.Write([]byte(fmt.Sprintf("failedroll %s", ev.Reason)))
	case *bgammon.EventMoved:
		c.Write([]byte(fmt.Sprintf("moved %s %s", ev.Player, bgammon.FormatAndFlipMoves(ev.Moves, c.playerNumber, bgammon.VariantBackgammon))))
	case *bgammon.EventFailedMove:
		c.Write([]byte(fmt.Sprintf("failedmove %d/%d %s", ev.From, ev.To, ev.Reason)))
	case *bgammon.EventFailedOk:
		c.Write([]byte(fmt.Sprintf("failedok %s", ev.Reason)))
	case *bgammon.EventWin:
		if ev.Points != 0 {
			c.Write([]byte(fmt.Sprintf("win %s wins %d points!", ev.Player, ev.Points)))
		} else {
			c.Write([]byte(fmt.Sprintf("win %s wins!", ev.Player)))
		}
	default:
		log.Printf("warning: skipped sending unknown event to non-json client: %+v", ev)
	}
}

func (c *serverClient) sendNotice(message string) {
	c.sendEvent(&bgammon.EventNotice{
		Message: message,
	})
}

func (c *serverClient) sendBroadcast(message string) {
	c.sendEvent(&bgammon.EventNotice{
		Message: gotext.GetD(c.language, "SERVER BROADCAST:") + " " + message,
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
	c.sendNotice("Connection terminated" + extra)

	go func() {
		time.Sleep(time.Second)
		c.Client.Terminate(reason)
	}()
}

func logClientRead(msg []byte) {
	msgLower := bytes.ToLower(msg)
	loginJSON := bytes.HasPrefix(msgLower, []byte("loginjson ")) || bytes.HasPrefix(msgLower, []byte("lj "))
	if bytes.HasPrefix(msgLower, []byte("login ")) || bytes.HasPrefix(msgLower, []byte("l ")) || loginJSON {
		split := bytes.Split(msg, []byte(" "))
		var clientName []byte
		var username []byte
		var password []byte
		l := len(split)
		if l > 1 {
			if loginJSON {
				clientName = split[1]
			} else {
				username = split[1]
			}
			if l > 2 {
				if loginJSON {
					username = split[2]
				} else {
					password = []byte("*******")
				}
				if l > 3 {
					if loginJSON {
						password = []byte("*******")
					}
				}
			}
		}
		if len(clientName) == 0 {
			clientName = []byte("unspecified")
		}
		log.Printf("<- %s %s %s %s", split[0], clientName, username, password)
	} else if !bytes.HasPrefix(msgLower, []byte("list")) && !bytes.HasPrefix(msgLower, []byte("ls")) && !bytes.HasPrefix(msgLower, []byte("pong")) {
		log.Printf("<- %s", msg)
	}
}
