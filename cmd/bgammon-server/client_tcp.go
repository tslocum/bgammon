package main

import (
	"bufio"
	"fmt"
	"log"
	"net"

	"code.rocket9labs.com/tslocum/bgammon"
)

var _ bgammon.Client = &socketClient{}

type socketClient struct {
	conn     net.Conn
	events   <-chan []byte
	commands chan<- []byte
}

func newSocketClient(conn net.Conn, commands chan<- []byte, events <-chan []byte) *socketClient {
	c := &socketClient{
		conn:     conn,
		events:   events,
		commands: commands,
	}
	go c.readCommands()
	go c.writeEvents()
	return c
}

func (c *socketClient) readCommands() {
	var scanner = bufio.NewScanner(c.conn)
	for scanner.Scan() {
		buf := make([]byte, len(scanner.Bytes()))
		copy(buf, scanner.Bytes())
		c.commands <- buf

		log.Printf("<- %s", scanner.Bytes())
	}
}

func (c *socketClient) writeEvents() {
	var event []byte
	for event = range c.events {
		c.conn.Write(event)
		c.conn.Write([]byte("\n"))

		log.Printf("-> %s", event)
	}
}

func (c *socketClient) Terminate(reason string) error {
	c.conn.Write([]byte(fmt.Sprintf("Connection closed: %s\n", reason)))
	c.conn.Close()
	return nil
}
