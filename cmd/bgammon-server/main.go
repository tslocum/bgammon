package main

import (
	"log"

	"code.rocket9labs.com/tslocum/bgammon"
)

func main() {
	b := bgammon.NewBoard()
	log.Printf("%+v", b)

	s := newServer()
	go s.listen("tcp", "127.0.0.1:1337")

	select {}
}
