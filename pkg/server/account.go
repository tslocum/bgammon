package server

type account struct {
	id       int
	email    []byte
	username []byte
	password []byte

	autoplay    bool
	highlight   bool
	pips        bool
	moves       bool
	flip        bool
	traditional bool
	advanced    bool
	speed       int8

	casual      *clientRating
	competitive *clientRating
}
