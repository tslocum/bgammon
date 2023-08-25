package main

import (
	"bufio"
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

	go s.handlePingClient(c)
	s.handleClientCommands(c)
}

func (s *server) handlePingClient(c *serverClient) {
	// TODO only ping when there is no recent activity
	t := time.NewTicker(time.Minute * 2)
	for {
		<-t.C

		if len(c.name) == 0 {
			c.Terminate("User did not send login command within 2 minutes.")
			return
		}

		c.lastPing = time.Now().Unix()
		c.events <- []byte(fmt.Sprintf("ping %d", c.lastPing))
	}
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
			if keyword == bgammon.CommandLogin || keyword == "l" {
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

		clientGame := s.gameByClient(cmd.client)

		switch keyword {
		case bgammon.CommandHelp, "h":
			// TODO get extended help by specifying a command after help
			cmd.client.events <- []byte("helpstart Help text:")
			cmd.client.events <- []byte("help Test help text")
			cmd.client.events <- []byte("helpend End of help text.")
		case bgammon.CommandJSON:
			sendUsage := func() {
				cmd.client.events <- []byte("notice To enable JSON formatted messages, send 'json on'. To disable JSON formatted messages, send 'json off'.")
			}
			if len(params) != 1 {
				sendUsage()
				continue
			}
			paramLower := strings.ToLower(string(params[0]))
			switch paramLower {
			case "on":
				cmd.client.json = true
				cmd.client.events <- []byte("json JSON formatted messages enabled.")
			case "off":
				cmd.client.json = false
				cmd.client.events <- []byte("json JSON formatted messages disabled.")
			default:
				sendUsage()
			}
		case bgammon.CommandSay, "s":
			if len(params) == 0 {
				continue
			}
			if clientGame == nil {
				cmd.client.events <- []byte("notice Message not sent. You are not currently in a game.")
				continue
			}
			opponent := clientGame.opponent(cmd.client)
			if opponent == nil {
				cmd.client.events <- []byte("notice Message not sent. There is no one else in the game.")
				continue
			}
			opponent.events <- []byte(fmt.Sprintf("say %s %s", cmd.client.name, bytes.Join(params, []byte(" "))))
		case bgammon.CommandList, "ls":
			cmd.client.events <- []byte("liststart Games list:")
			players := 0
			password := 0
			name := "game name"
			for _, g := range s.games {
				cmd.client.events <- []byte(fmt.Sprintf("game %d %d %d %s", g.id, password, players, name))
			}
			cmd.client.events <- []byte("listend End of games list.")
		case bgammon.CommandCreate, "c":
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
			if !g.addClient(cmd.client) {
				log.Panicf("failed to add client to newly created game %+v %+v", g, cmd.client)
			}
			s.games = append(s.games, g) // TODO lock

			g.sendBoard(cmd.client)
		case bgammon.CommandJoin, "j":
			if clientGame != nil {
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
			gameID, err := strconv.Atoi(string(params[0]))
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

					if !g.addClient(cmd.client) {
						cmd.client.events <- []byte("failedjoin Game is full.")
						continue COMMANDS
					}

					g.sendBoard(cmd.client)
					continue COMMANDS
				}
			}
		case bgammon.CommandLeave, "l":
			if clientGame == nil {
				cmd.client.events <- []byte("failedleave You are not currently in a game.")
				continue
			}
			if clientGame.client1 == cmd.client {
				clientGame.client1 = nil
			} else {
				clientGame.client2 = nil
			}
			// TODO handle pausing or ending game
			// TODO move to .removeClient

			leftMessage := []byte(fmt.Sprintf("left %s", cmd.client.name))
			cmd.client.events <- leftMessage
			opponent := clientGame.opponent(cmd.client)
			if opponent != nil {
				opponent.events <- leftMessage
			}
		case bgammon.CommandRoll, "r":
			if clientGame == nil {
				cmd.client.events <- []byte("notice You are not currently in a game.")
				continue
			}

			playerNumber := 1
			if clientGame.client2 == cmd.client {
				playerNumber = 2
			}
			if !clientGame.roll(playerNumber) {
				cmd.client.events <- []byte("notice It is not your turn to roll.")
			} else {
				clientGame.eachClient(func(client *serverClient) {
					client.events <- []byte(fmt.Sprintf("rolled %d %d", clientGame.Roll1, clientGame.Roll2))
				})
			}
		case bgammon.CommandMove, "m", "mv":
			if clientGame == nil {
				cmd.client.events <- []byte("notice You are not currently in a game.")
				continue
			}

			sendUsage := func() {
				cmd.client.events <- []byte("notice Specify one or more moves in the form FROM/TO. For example: 8/4 6/4")
			}

			if len(params) == 0 {
				sendUsage()
				continue
			}

			gameCopy := bgammon.Game{}
			gameCopy = *clientGame.Game
			gameCopy.Moves = [][]int{}
			copy(gameCopy.Moves, clientGame.Moves)

			var moves [][]int
			for i := range params {
				split := bytes.Split(params[i], []byte("/"))
				if len(split) != 2 {
					sendUsage()
					continue COMMANDS
				}
				from, err := strconv.Atoi(string(split[0]))
				if err != nil {
					sendUsage()
					continue COMMANDS
				}
				to, err := strconv.Atoi(string(split[1]))
				if err != nil {
					sendUsage()
					continue COMMANDS
				}

				legalMoves := gameCopy.LegalMoves()
				var found bool
				for j := range legalMoves {
					if legalMoves[j][0] == from && legalMoves[j][1] == to {
						found = true
						break
					}
				}
				if !found {
					cmd.client.events <- []byte(fmt.Sprintf("failedmove %d/%d Illegal move.", from, to))
					continue COMMANDS
				}

				move := []int{from, to}
				moves = append(moves, move)
				gameCopy.Moves = append(gameCopy.Moves, move)
			}

			paramsText := bytes.Join(params, []byte(" "))
			clientGame.Moves = gameCopy.Moves
			clientGame.eachClient(func(client *serverClient) {
				client.events <- []byte(fmt.Sprintf("move %s %s", cmd.client.name, paramsText))
				clientGame.sendBoard(client)
			})
		case bgammon.CommandBoard, "b":
			if clientGame == nil {
				cmd.client.events <- []byte("notice You are not currently in a game.")
			} else {
				playerNumber := 1
				if clientGame.client2 == cmd.client {
					playerNumber = 2
				}

				scanner := bufio.NewScanner(bytes.NewReader(clientGame.BoardState(playerNumber)))
				for scanner.Scan() {
					cmd.client.events <- append([]byte("notice "), scanner.Bytes()...)
				}
			}
		case bgammon.CommandDisconnect:
			if clientGame != nil {
				clientGame.removeClient(cmd.client)
			}
			cmd.client.Terminate("Client disconnected")
		default:
			log.Printf("unknown command %s", keyword)
		}
	}
}
