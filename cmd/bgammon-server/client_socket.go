package main

import (
	"bufio"
	"bytes"
	"log"
	"net"
	"sync"
	"time"

	"code.rocket9labs.com/tslocum/bgammon"
)

var _ bgammon.Client = &socketClient{}

type socketClient struct {
	conn       net.Conn
	events     chan []byte
	commands   chan<- []byte
	terminated bool
	wgEvents   sync.WaitGroup
}

func newSocketClient(conn net.Conn, commands chan<- []byte, events chan []byte) *socketClient {
	return &socketClient{
		conn:     conn,
		events:   events,
		commands: commands,
	}
}

func (c *socketClient) HandleReadWrite() {
	if c.terminated {
		return
	}

	closeWrite := make(chan struct{}, 1)

	go c.writeEvents(closeWrite)
	c.readCommands()

	closeWrite <- struct{}{}
}

func (c *socketClient) Write(message []byte) {
	if c.terminated {
		return
	}

	c.wgEvents.Add(1)
	c.events <- message
}

func (c *socketClient) readCommands() {
	setTimeout := func() {
		err := c.conn.SetReadDeadline(time.Now().Add(clientTimeout))
		if err != nil {
			c.Terminate(err.Error())
			return
		}
	}

	setTimeout()
	var scanner = bufio.NewScanner(c.conn)
	for scanner.Scan() {
		if c.terminated {
			return
		}

		if scanner.Err() != nil {
			c.Terminate(scanner.Err().Error())
			return
		}

		buf := make([]byte, len(scanner.Bytes()))
		copy(buf, scanner.Bytes())
		c.commands <- buf

		logClientRead(scanner.Bytes())

		setTimeout()
	}
}

func (c *socketClient) writeEvents(closeWrite chan struct{}) {
	setTimeout := func() {
		err := c.conn.SetWriteDeadline(time.Now().Add(clientTimeout))
		if err != nil {
			c.Terminate(err.Error())
			return
		}
	}

	setTimeout()
	var event []byte
	for {
		select {
		case <-closeWrite:
			for {
				select {
				case <-c.events:
					c.wgEvents.Done()
				default:
					return
				}
			}
		case event = <-c.events:
		}

		if c.terminated {
			c.wgEvents.Done()
			continue
		}

		setTimeout()
		_, err := c.conn.Write(append(event, '\n'))
		if err != nil {
			c.Terminate(err.Error())
			c.wgEvents.Done()
			continue
		}

		if !bytes.HasPrefix(event, []byte(`{"Type":"ping"`)) && !bytes.HasPrefix(event, []byte(`{"Type":"list"`)) {
			log.Printf("-> %s", event)
		}
		c.wgEvents.Done()
	}
}

func (c *socketClient) Terminate(reason string) {
	if c.terminated {
		return
	}
	c.terminated = true
	c.conn.Close()
}

func (c *socketClient) Terminated() bool {
	return c.terminated
}
