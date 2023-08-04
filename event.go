package bgammon

// events are always received FROM the server

type Event struct {
	Player int
	Command
}
