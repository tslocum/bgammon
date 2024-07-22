package bgammon

import (
	"encoding/json"
	"fmt"
)

const (
	SpeedSlow    int8 = 0
	SpeedMedium  int8 = 1
	SpeedFast    int8 = 2
	SpeedInstant int8 = 3
)

type Event struct {
	Type   string
	Player string
}

type EventWelcome struct {
	Event
	PlayerName string
	Clients    int
	Games      int
}

type EventPing struct {
	Event
	Message string
}

type EventNotice struct {
	Event
	Message string
}

type EventSay struct {
	Event
	Message string
}

type GameListing struct {
	ID       int
	Password bool
	Points   int8
	Players  int8
	Rating   int
	Name     string
}

type EventList struct {
	Event
	Games []GameListing
}

type EventJoined struct {
	Event
	GameID       int
	PlayerNumber int8
}

type EventFailedJoin struct {
	Event
	Reason string
}

type EventLeft struct {
	Event
}

type EventFailedLeave struct {
	Event
	Reason string
}

type EventBoard struct {
	Event
	GameState
}

type EventRolled struct {
	Event
	Roll1    int8
	Roll2    int8
	Roll3    int8 // Used in tabula games.
	Selected bool // Whether the roll is selected by the player (used in acey-deucey games).
}

type EventFailedRoll struct {
	Event
	Reason string
}

type EventMoved struct {
	Event
	Moves [][]int8
}

type EventFailedMove struct {
	Event
	From   int8
	To     int8
	Reason string
}

type EventFailedOk struct {
	Event
	Reason string
}

type EventWin struct {
	Event
	Points int8
}

type EventSettings struct {
	Event
	AutoPlay      bool
	Highlight     bool
	Pips          bool
	Moves         bool
	Flip          bool
	Traditional   bool
	Advanced      bool
	MuteJoinLeave bool
	MuteChat      bool
	MuteRoll      bool
	MuteMove      bool
	MuteBearOff   bool
	Speed         int8
}

type EventReplay struct {
	Event
	ID      int
	Content []byte
}

type HistoryMatch struct {
	ID        int
	Timestamp int64
	Points    int8
	Opponent  string
	Winner    int8
}

type EventHistory struct {
	Event
	Page                   int
	Pages                  int
	Matches                []*HistoryMatch
	CasualBackgammonSingle int
	CasualBackgammonMulti  int
	CasualAceyDeuceySingle int
	CasualAceyDeuceyMulti  int
	CasualTabulaSingle     int
	CasualTabulaMulti      int
}

func DecodeEvent(message []byte) (interface{}, error) {
	e := &Event{}
	err := json.Unmarshal(message, e)
	if err != nil {
		return nil, err
	}

	var ev interface{}
	switch e.Type {
	case EventTypeWelcome:
		ev = &EventWelcome{}
	case EventTypePing:
		ev = &EventPing{}
	case EventTypeNotice:
		ev = &EventNotice{}
	case EventTypeSay:
		ev = &EventSay{}
	case EventTypeList:
		ev = &EventList{}
	case EventTypeJoined:
		ev = &EventJoined{}
	case EventTypeFailedJoin:
		ev = &EventFailedJoin{}
	case EventTypeLeft:
		ev = &EventLeft{}
	case EventTypeFailedLeave:
		ev = &EventFailedLeave{}
	case EventTypeBoard:
		ev = &EventBoard{}
	case EventTypeRolled:
		ev = &EventRolled{}
	case EventTypeFailedRoll:
		ev = &EventFailedRoll{}
	case EventTypeMoved:
		ev = &EventMoved{}
	case EventTypeFailedMove:
		ev = &EventFailedMove{}
	case EventTypeFailedOk:
		ev = &EventFailedOk{}
	case EventTypeWin:
		ev = &EventWin{}
	case EventTypeSettings:
		ev = &EventSettings{}
	case EventTypeReplay:
		ev = &EventReplay{}
	case EventTypeHistory:
		ev = &EventHistory{}
	default:
		return nil, fmt.Errorf("failed to decode event: unknown event type: %s", e.Type)
	}

	err = json.Unmarshal(message, ev)
	if err != nil {
		return nil, err
	}
	return ev, nil
}
