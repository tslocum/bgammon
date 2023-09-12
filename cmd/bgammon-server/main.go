package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
)

func main() {
	var (
		tcpAddress string
		wsAddress  string
		debug      int
	)
	flag.StringVar(&tcpAddress, "tcp", "localhost:1337", "TCP listen address")
	flag.StringVar(&wsAddress, "ws", "localhost:1338", "WebSocket listen address")
	flag.IntVar(&debug, "debug", 0, "print debug information and serve pprof on specified port")
	flag.Parse()

	if tcpAddress == "" && wsAddress == "" {
		log.Fatal("Error: A TCP and/or WebSocket listen address must be specified.")
	}

	if debug > 0 {
		go func() {
			log.Fatal(http.ListenAndServe(fmt.Sprintf("localhost:%d", debug), nil))
		}()
	}

	s := newServer()
	if tcpAddress != "" {
		s.listen("tcp", tcpAddress)
	}
	if wsAddress != "" {
		s.listen("ws", wsAddress)
	}
	select {}
}
