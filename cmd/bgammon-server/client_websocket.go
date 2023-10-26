package main

import (
	"bytes"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"code.rocket9labs.com/tslocum/bgammon"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

var _ bgammon.Client = &webSocketClient{}

type webSocketClient struct {
	conn       net.Conn
	events     chan []byte
	commands   chan<- []byte
	terminated bool
	wgEvents   sync.WaitGroup
}

func newWebSocketClient(r *http.Request, w http.ResponseWriter, commands chan<- []byte, events chan []byte) *webSocketClient {
	conn, _, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		return nil
	}

	return &webSocketClient{
		conn:     conn,
		events:   events,
		commands: commands,
	}
}

func (c *webSocketClient) HandleReadWrite() {
	if c.terminated {
		return
	}

	go c.writeEvents()
	c.readCommands()
}

func (c *webSocketClient) Write(message []byte) {
	if c.terminated {
		return
	}

	c.wgEvents.Add(1)
	c.events <- message
}

func (c *webSocketClient) readCommands() {
	setTimeout := func() {
		err := c.conn.SetReadDeadline(time.Now().Add(clientTimeout))
		if err != nil {
			c.Terminate(err.Error())
			return
		}
	}

	setTimeout()
	for {
		if c.terminated {
			continue // TODO wait group
		}

		msg, op, err := wsutil.ReadClientData(c.conn)
		if err != nil {
			c.Terminate(err.Error())
			return
		} else if op != ws.OpText {
			continue
		}

		buf := make([]byte, len(msg))
		copy(buf, msg)
		c.commands <- buf

		logClientRead(msg)
		setTimeout()
	}
}

func (c *webSocketClient) writeEvents() {
	setTimeout := func() {
		err := c.conn.SetWriteDeadline(time.Now().Add(clientTimeout))
		if err != nil {
			c.Terminate(err.Error())
			return
		}
	}

	setTimeout()
	var event []byte
	for event = range c.events {
		if c.terminated {
			c.wgEvents.Done()
			continue
		}
		setTimeout()

		err := wsutil.WriteServerMessage(c.conn, ws.OpText, event)
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

func (c *webSocketClient) Terminate(reason string) {
	if c.terminated {
		return
	}
	c.terminated = true
	c.conn.Close()
	go func() {
		time.Sleep(5 * time.Second)
		c.wgEvents.Wait()
		close(c.events)
		close(c.commands)
	}()
}

func (c *webSocketClient) Terminated() bool {
	return c.terminated
}
