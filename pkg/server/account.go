package server

type account struct {
	id        int
	email     []byte
	username  []byte
	password  []byte
	highlight bool
	pips      bool
	moves     bool
}
