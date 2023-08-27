package bgammon

type Client interface {
	Write(message []byte)
	Terminate(reason string)
	Terminated() bool
}
