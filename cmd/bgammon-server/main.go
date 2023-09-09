package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
)

func main() {
	var tcpAddress string
	var debug int
	flag.StringVar(&tcpAddress, "tcp", "localhost:1337", "TCP listen address")
	flag.IntVar(&debug, "debug", 0, "print debug information and serve pprof on specified port")
	flag.Parse()

	if debug > 0 {
		go func() {
			log.Fatal(http.ListenAndServe(fmt.Sprintf("localhost:%d", debug), nil))
		}()
	}

	s := newServer()
	s.listen("tcp", tcpAddress)
	select {}
}
