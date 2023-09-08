package main

import "flag"

func main() {
	var address string
	flag.StringVar(&address, "tcp", "localhost:1337", "TCP listen address")
	flag.Parse()

	s := newServer()
	s.listen("tcp", address)
	select {}
}
