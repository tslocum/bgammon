//go:build full

package server

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"sync"

	"code.rocket9labs.com/tslocum/bgammon"
	"github.com/coder/websocket"
)

var acceptOptions = &websocket.AcceptOptions{
	InsecureSkipVerify: true,
	CompressionMode:    websocket.CompressionContextTakeover,
}

var _ bgammon.Client = &webSocketClient{}

type webSocketClient struct {
	conn       *websocket.Conn
	address    string
	events     chan []byte
	commands   chan<- []byte
	terminated bool
	wgEvents   sync.WaitGroup
	verbose    bool
}

func newWebSocketClient(r *http.Request, w http.ResponseWriter, commands chan<- []byte, events chan []byte, verbose bool) *webSocketClient {
	conn, err := websocket.Accept(w, r, acceptOptions)
	if err != nil {
		return nil
	}

	return &webSocketClient{
		conn:     conn,
		address:  hashIP(r.RemoteAddr),
		events:   events,
		commands: commands,
		verbose:  verbose,
	}
}

func (c *webSocketClient) Address() string {
	return c.address
}

func (c *webSocketClient) HandleReadWrite() {
	if c.terminated {
		return
	}

	closeWrite := make(chan struct{}, 1)

	go c.writeEvents(closeWrite)
	c.readCommands()

	closeWrite <- struct{}{}
}

func (c *webSocketClient) Write(message []byte) {
	if c.terminated {
		return
	}

	c.wgEvents.Add(1)
	c.events <- message
}

func (c *webSocketClient) readCommands() {
	var ctx context.Context
	for {
		if c.terminated {
			return
		}

		ctx, _ = context.WithTimeout(context.Background(), clientTimeout)
		msgType, msgContent, err := c.conn.Read(ctx)
		if err != nil {
			c.Terminate(err.Error())
			return
		} else if msgType != websocket.MessageText {
			continue
		}

		buf := make([]byte, len(msgContent))
		copy(buf, msgContent)
		c.commands <- buf

		if c.verbose {
			logClientRead(msgContent)
		}
	}
}

func (c *webSocketClient) writeEvents(closeWrite chan struct{}) {
	var ctx context.Context
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

		ctx, _ = context.WithTimeout(context.Background(), clientTimeout)
		err := c.conn.Write(ctx, websocket.MessageText, event)
		if err != nil {
			c.Terminate(err.Error())
			c.wgEvents.Done()
			continue
		}

		if c.verbose && !bytes.HasPrefix(event, []byte(`{"Type":"ping"`)) && !bytes.HasPrefix(event, []byte(`{"Type":"list"`)) {
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
	c.conn.CloseNow()
}

func (c *webSocketClient) Terminated() bool {
	return c.terminated
}
