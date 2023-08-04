package main

import (
	"bytes"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strings"
	"time"

	"code.rocket9labs.com/tslocum/bgammon"
)

type serverGame struct {
	id         int
	created    int64
	lastActive int64
	client1    bgammon.Client
	client2    bgammon.Client
	bgammon.Game
}

type serverCommand struct {
	client  *serverClient
	command []byte
}

type server struct {
	clients      []*serverClient
	games        []*serverGame
	listeners    []net.Listener
	newClientIDs chan int
	commands     chan serverCommand
}

func newServer() *server {
	const bufferSize = 10
	s := &server{
		newClientIDs: make(chan int),
		commands:     make(chan serverCommand, bufferSize),
	}
	go s.handleNewClientIDs()
	go s.handleCommands()
	return s
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

	const bufferSize = 8
	commands := make(chan []byte, bufferSize)
	events := make(chan []byte, bufferSize)

	now := time.Now().Unix()

	c := &serverClient{
		id:         <-s.newClientIDs,
		account:    -1,
		connected:  now,
		lastActive: now,
		commands:   commands,
		events:     events,
		Client:     newSocketClient(conn, commands, events),
	}
	log.Println("socket client", c)

	s.handleClientCommands(c)
}

func (s *server) handleClientCommands(c *serverClient) {
	var command []byte
	for command = range c.commands {
		s.commands <- serverCommand{
			client:  c,
			command: command,
		}
	}
}

func (s *server) handleNewClientIDs() {
	clientID := 1
	for {
		s.newClientIDs <- clientID
		clientID++
	}
}

func (s *server) randomUsername() []byte {
	// TODO check if username is available
	i := 100 + rand.Intn(900)
	return []byte(fmt.Sprintf("Guest%d", i))
}

func (s *server) sendWelcome(c *serverClient) {
	c.events <- []byte(fmt.Sprintf("welcome %s there are %d clients playing %d games", c.name, len(s.clients), len(s.games)))
}

func (s *server) handleCommands() {
	var cmd serverCommand
	for cmd = range s.commands {
		if cmd.client == nil {
			log.Panicf("nil client with command %s", cmd.command)
		}

		cmd.command = bytes.TrimSpace(cmd.command)

		firstSpace := bytes.IndexByte(cmd.command, ' ')
		var keyword string
		var startParameters int
		if firstSpace == -1 {
			keyword = string(cmd.command)
			startParameters = len(cmd.command)
		} else {
			keyword = string(cmd.command[:firstSpace])
			startParameters = firstSpace + 1
		}
		if keyword == "" {
			continue
		}
		keyword = strings.ToLower(keyword)

		log.Printf("server client %+v command %s with keyword %s", cmd.client, cmd.command, keyword)

		params := bytes.Fields(cmd.command[startParameters:])
		log.Printf("params %+v", params)

		// Require users to send login command first.
		if cmd.client.account == -1 {
			if keyword == bgammon.CommandLogin {
				var username []byte
				var password []byte
				switch len(params) {
				case 0:
					username = s.randomUsername()
				case 1:
					username = params[0]
				default:
					username = params[0]
					password = bytes.Join(params[1:], []byte(" "))
				}

				if len(password) > 0 {
					cmd.client.account = 1
				} else {
					cmd.client.account = 0
				}
				cmd.client.name = username

				s.sendWelcome(cmd.client)

				log.Printf("login as %s - %s", username, password)
				continue
			} else {
				cmd.client.Terminate("You must login before using other commands.")
				continue
			}
		}

		switch keyword {
		case bgammon.CommandSay:
			if len(params) == 0 {
				continue
			}
		case bgammon.CommandList:
		case bgammon.CommandCreate:
			sendUsage := func() {
				cmd.client.events <- []byte("notice To create a public game specify whether it is public or private.")
				cmd.client.events <- []byte("notice When creating a private game, a password must be set.")
			}
			if len(params) == 0 {
				sendUsage()
				continue
			}
			var gamePassword []byte
			gameType := bytes.ToLower(params[0])
			var gameName []byte
			switch {
			case bytes.Equal(gameType, []byte("public")):
				gameName = bytes.Join(params[1:], []byte(" "))
			case bytes.Equal(gameType, []byte("private")):
				if len(params) < 2 {
					sendUsage()
					continue
				}
				gamePassword = params[1]
				gameName = bytes.Join(params[2:], []byte(" "))
			default:
				sendUsage()
				continue
			}
			log.Printf("create game (password %s) name: %s", gamePassword, gameName)
		case bgammon.CommandJoin:
		case bgammon.CommandRoll:
		case bgammon.CommandMove:
		default:
			log.Printf("unknown command %s", keyword)
		}
	}
}
