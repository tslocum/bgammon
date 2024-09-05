package bgammon

type Client interface {
	Address() string
	HandleReadWrite()
	Write(message []byte)
	Terminate(reason string)
	Terminated() bool
}
