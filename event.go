package bgammon

import (
	"encoding/json"
	"fmt"
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

type EventHelp struct {
	Event
	Topic   string
	Message string
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
	Points   int
	Players  int
	Name     string
}

type EventList struct {
	Event
	Games []GameListing
}

type EventJoined struct {
	Event
	GameID       int
	PlayerNumber int
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
	Roll1    int
	Roll2    int
	Selected bool // Whether the roll is selected by the player (used in acey-deucey games).
}

type EventFailedRoll struct {
	Event
	Reason string
}

type EventMoved struct {
	Event
	Moves [][]int
}

type EventFailedMove struct {
	Event
	From   int
	To     int
	Reason string
}

type EventFailedOk struct {
	Event
	Reason string
}

type EventWin struct {
	Event
	Points int
}

type EventSettings struct {
	Event
	Highlight bool
	Pips      bool
	Moves     bool
}

type EventReplay struct {
	Event
	ID      int
	Content []byte
}

type HistoryMatch struct {
	ID        int
	Timestamp int64
	Points    int
	Opponent  string
	Winner    int
}

type EventHistory struct {
	Event
	Matches []*HistoryMatch
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
	case EventTypeHelp:
		ev = &EventHelp{}
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
