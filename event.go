package bgammon

import (
	"encoding/json"
	"fmt"
)

// events are always received FROM the server

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
	Event
	ID       int
	Password bool
	Players  int
	Name     string
}

type EventList struct {
	Event
	Games []GameListing
}

type EventJoined struct {
	Event
	GameID int
}

type EventFailedJoin struct {
	Event
	Reason string
}

type EventBoard struct {
	Event
	GameState
}

type EventRolled struct {
	Event
	Roll1 int
	Roll2 int
}

type EventMoved struct {
	Event
	Moves [][]int
}

func DecodeEvent(message []byte) (interface{}, error) {
	e := &Event{}
	err := json.Unmarshal(message, e)
	if err != nil {
		return nil, err
	}
	switch e.Type {
	case EventTypeWelcome:
		ev := &EventWelcome{}
		err = json.Unmarshal(message, ev)
		if err != nil {
			return nil, err
		}
		return ev, nil
	case EventTypePing:
		ev := &EventPing{}
		err = json.Unmarshal(message, ev)
		if err != nil {
			return nil, err
		}
		return ev, nil
	case EventTypeNotice:
		ev := &EventNotice{}
		err = json.Unmarshal(message, ev)
		if err != nil {
			return nil, err
		}
		return ev, nil
	case EventTypeSay:
		ev := &EventSay{}
		err = json.Unmarshal(message, ev)
		if err != nil {
			return nil, err
		}
		return ev, nil
	case EventTypeList:
		ev := &EventList{}
		err = json.Unmarshal(message, ev)
		if err != nil {
			return nil, err
		}
		return ev, nil
	case EventTypeJoined:
		ev := &EventJoined{}
		err = json.Unmarshal(message, ev)
		if err != nil {
			return nil, err
		}
		return ev, nil
	case EventTypeFailedJoin:
		ev := &EventFailedJoin{}
		err = json.Unmarshal(message, ev)
		if err != nil {
			return nil, err
		}
		return ev, nil
	case EventTypeBoard:
		ev := &EventBoard{}
		err = json.Unmarshal(message, ev)
		if err != nil {
			return nil, err
		}
		return ev, nil
	case EventTypeRolled:
		ev := &EventRolled{}
		err = json.Unmarshal(message, ev)
		if err != nil {
			return nil, err
		}
		return ev, nil
	case EventTypeMoved:
		ev := &EventMoved{}
		err = json.Unmarshal(message, ev)
		if err != nil {
			return nil, err
		}
		return ev, nil
	default:
		return nil, fmt.Errorf("failed to decode event: unknown event type: %s", e.Type)
	}
}
