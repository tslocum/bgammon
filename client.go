package bgammon

type Client interface {
	ReadCommand() (Command, error)
	WriteEvent() (Event, error)
	Terminate() error
}
