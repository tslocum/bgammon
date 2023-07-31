package bgammon

// commands are always sent TO the server

type Command struct {
	Type int
}

type CommandChat struct {
	Command
	Message string
}

type CommandMove struct {
	Event
	Spaces []int // One or more sets of moves from A->B as A,B,A,B,A,B...
}
