package main

import (
	"log"
	"net"

	"code.rocket9labs.com/tslocum/bgammon"
)

type serverClient struct {
	id         int
	connected  int64
	lastActive int64
	bgammon.Client
}

type serverGame struct {
	id         int
	created    int64
	lastActive int64
	client1    bgammon.Client
	client2    bgammon.Client
	bgammon.Game
}

type server struct {
	clients   []*serverClient
	games     []*serverGame
	listeners []net.Listener
}

func newServer() *server {
	return &server{}
}

func (s *server) listen(network string, address string) {
	log.Printf("Listening for %s connections on %s...", network, address)
	listener, err := net.Listen(network, address)
	if err != nil {
		log.Fatalf("failed to listen on %s: %s", address, err)
	}
	go s.handleListener(listener)
	s.listeners = append(s.listeners, listener)
}

func (s *server) handleListener(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Fatalf("failed to accept connection: %s", err)
		}
		go s.handleConnection(conn)
	}
}

func (s *server) handleConnection(conn net.Conn) {
	log.Printf("new conn %+v", conn)
}
