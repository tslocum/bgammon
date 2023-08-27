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
	PlayerName string
	Clients    int
	Games      int
}

type EventJoined struct {
	GameID     int
	PlayerName string
}

type GameListing struct {
	ID       int
	Password bool
	Players  int
	Name     string
}

type EventList struct {
	Games []GameListing
}

type EventSay struct {
	Message string
}

type EventBoard struct {
	GameState
}

type EventRoll struct {
	Roll1 int
	Roll2 int
}

type EventMove struct {
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
	case EventTypeJoined:
		ev := &EventJoined{}
		err = json.Unmarshal(message, ev)
		if err != nil {
			return nil, err
		}
		return ev, nil
	default:
		return nil, fmt.Errorf("failed to decode event: unknown event type: %s", e.Type)
	}
}
