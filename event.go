package bgammon

// events are always received FROM the server

type Event struct {
	Type   int
	Player int
}

type EventChat struct {
	Event
	Message string
}

type EventMove struct {
	Event
	Spaces []int // One or more sets of moves from A->B as A,B,A,B,A,B...
}
