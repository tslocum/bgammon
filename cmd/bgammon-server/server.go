package main

import (
	"bytes"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"time"

	"code.rocket9labs.com/tslocum/bgammon"
)

type serverCommand struct {
	client  *serverClient
	command []byte
}

type server struct {
	clients      []*serverClient
	games        []*serverGame
	listeners    []net.Listener
	newGameIDs   chan int
	newClientIDs chan int
	commands     chan serverCommand
}

func newServer() *server {
	const bufferSize = 10
	s := &server{
		newGameIDs:   make(chan int),
		newClientIDs: make(chan int),
		commands:     make(chan serverCommand, bufferSize),
	}
	go s.handleNewGameIDs()
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

	s.sendHello(c)

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

func (s *server) handleNewGameIDs() {
	gameID := 1
	for {
		s.newGameIDs <- gameID
		gameID++
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

func (s *server) sendHello(c *serverClient) {
	c.events <- []byte("hello Welcome to bgammon.org! Please log in by sending the 'login' command. You may specify a username, otherwise you will be assigned a random username. If you specify a username, you may also specify a password. Have fun!")
}

func (s *server) sendWelcome(c *serverClient) {
	c.events <- []byte(fmt.Sprintf("welcome %s there are %d clients playing %d games.", c.name, len(s.clients), len(s.games)))
}

func (s *server) gameByClient(c *serverClient) *serverGame {
	for _, g := range s.games {
		if g.client1 == c || g.client2 == c {
			return g
		}
	}
	return nil
}

func (s *server) handleCommands() {
	var cmd serverCommand
COMMANDS:
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
		case bgammon.CommandHelp:
			// TODO get extended help by specifying a command after help
			cmd.client.events <- []byte("helpstart Help text:")
			cmd.client.events <- []byte("help Test help text")
			cmd.client.events <- []byte("helpend End of help text.")
		case bgammon.CommandSay:
			if len(params) == 0 {
				continue
			}
		case bgammon.CommandList:
			cmd.client.events <- []byte("liststart Games list:")
			players := 0
			password := 0
			name := "game name"
			for _, g := range s.games {
				cmd.client.events <- []byte(fmt.Sprintf("game %d %d %d %s", g.id, password, players, name))
			}
			cmd.client.events <- []byte("listend End of games list.")
		case bgammon.CommandCreate:
			sendUsage := func() {
				cmd.client.events <- []byte("notice To create a public game specify whether it is public or private. When creating a private game, a password must also be provided.")
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

			g := newServerGame(<-s.newGameIDs)
			g.name = gameName
			g.password = gamePassword

			if rand.Intn(2) == 0 { // Start as black.
				g.client1 = cmd.client
				g.Player1.Name = string(cmd.client.name)
			} else { // Start as white.
				g.client2 = cmd.client
				g.Player2.Name = string(cmd.client.name)
			}

			s.games = append(s.games, g)

			players := 1
			password := 0
			if len(gamePassword) != 0 {
				password = 1
			}
			cmd.client.events <- []byte(fmt.Sprintf("joined %d %d %d %s", g.id, password, players, gameName))
		case bgammon.CommandJoin:
			if s.gameByClient(cmd.client) != nil {
				cmd.client.events <- []byte("failedjoin Please leave the game you are in before joining another game.")
				continue
			}

			sendUsage := func() {
				cmd.client.events <- []byte("notice To join a public game specify its game ID. To join a private game, a password must also be specified.")
			}

			if len(params) == 0 {
				sendUsage()
				continue
			}
			gameID, err := strconv.Atoi(string(params[1]))
			if err != nil || gameID < 1 {
				sendUsage()
				continue
			}

			for _, g := range s.games {
				if g.id == gameID {
					if len(g.password) != 0 && (len(params) < 2 || !bytes.Equal(g.password, bytes.Join(params[2:], []byte(" ")))) {
						cmd.client.events <- []byte("failedjoin Invalid password.")
						continue COMMANDS
					}

					log.Printf("join existing game %+v", g)
					// cmd.client.events <- []byte(fmt.Sprintf("joined %d %d %d %s", g.id, players, password, gameName))
					continue COMMANDS
				}
			}
		case bgammon.CommandLeave:
		case bgammon.CommandRoll:
		case bgammon.CommandMove:
		case bgammon.CommandDisconnect:
			g := s.gameByClient(cmd.client)
			if g != nil {
				// todo leave game
			}
			cmd.client.Terminate("Client disconnected")
		default:
			log.Printf("unknown command %s", keyword)
		}
	}
}
