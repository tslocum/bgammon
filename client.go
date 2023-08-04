package bgammon

type Client interface {
	Terminate(reason string) error
}
