package bgammon

type Client interface {
	HandleReadWrite()
	Write(message []byte)
	Terminate(reason string)
	Terminated() bool
}
