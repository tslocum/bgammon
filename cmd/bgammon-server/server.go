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

const clientTimeout = 10 * time.Minute

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
		Client:     newSocketClient(conn, commands, events),
	}
	log.Println("socket client", c)

	s.sendHello(c)

	go s.handlePingClient(c)
	s.handleClientCommands(c)
}

func (s *server) handlePingClient(c *serverClient) {
	// TODO only ping when there is no recent activity
	t := time.NewTicker(time.Minute * 4)
	for {
		<-t.C

		if c.Terminated() {
			t.Stop()
			return
		}

		if len(c.name) == 0 {
			c.Terminate("User did not send login command within 2 minutes.")
			t.Stop()
			return
		}

		c.lastPing = time.Now().Unix()
		c.sendEvent(&bgammon.EventPing{
			Message: fmt.Sprintf("%d", c.lastPing),
		})
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
	c.Write([]byte("hello Welcome to bgammon.org! Please log in by sending the 'login' command. You may specify a username, otherwise you will be assigned a random username. If you specify a username, you may also specify a password. Have fun!"))
}

func (s *server) sendWelcome(c *serverClient) {
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
			if keyword == bgammon.CommandLogin || keyword == bgammon.CommandLoginJSON || keyword == "l" || keyword == "lj" {
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

				if keyword == bgammon.CommandLoginJSON || keyword == "lj" {
					cmd.client.json = true
				}

				cmd.client.sendEvent(&bgammon.EventWelcome{
					PlayerName: string(cmd.client.name),
					Clients:    len(s.clients),
					Games:      len(s.games),
				})

				log.Printf("login as %s - %s", username, password)
				continue
			} else {
				cmd.client.Terminate("You must login before using other commands.")
				continue
			}
		}

		clientGame := s.gameByClient(cmd.client)
		clientNumber := 0
		if clientGame != nil {
			clientNumber = 1
			if clientGame.client2 == cmd.client {
				clientNumber = 2
			}
		}

		switch keyword {
		case bgammon.CommandHelp, "h":
			// TODO get extended help by specifying a command after help
			cmd.client.Write([]byte("helpstart Help text:"))
			cmd.client.Write([]byte("help Test help text"))
			cmd.client.Write([]byte("helpend End of help text."))
			// TODO JSON format
		case bgammon.CommandJSON:
			sendUsage := func() {
				cmd.client.Write([]byte("notice To enable JSON formatted messages, send 'json on'. To disable JSON formatted messages, send 'json off'."))
			}
			if len(params) != 1 {
				sendUsage()
				continue
			}
			paramLower := strings.ToLower(string(params[0]))
			switch paramLower {
			case "on":
				cmd.client.json = true
				cmd.client.Write([]byte("json JSON formatted messages enabled.")) // TODO send in JSON format
			case "off":
				cmd.client.json = false
				cmd.client.Write([]byte("json JSON formatted messages disabled."))
			default:
				sendUsage()
			}
		case bgammon.CommandSay, "s":
			if len(params) == 0 {
				continue
			}
			if clientGame == nil {
				cmd.client.Write([]byte("notice Message not sent. You are not currently in a game."))
				continue
			}
			opponent := clientGame.opponent(cmd.client)
			if opponent == nil {
				cmd.client.Write([]byte("notice Message not sent. There is no one else in the game."))
				continue
			}
			opponent.Write([]byte(fmt.Sprintf("say %s %s", cmd.client.name, bytes.Join(params, []byte(" ")))))
		case bgammon.CommandList, "ls":
			ev := &bgammon.EventList{}
			for _, g := range s.games {
				ev.Games = append(ev.Games, bgammon.GameListing{
					ID:       g.id,
					Password: len(g.password) != 0,
					Players:  g.playerCount(),
					Name:     string(g.name),
				})
			}
			cmd.client.sendEvent(ev)
		case bgammon.CommandCreate, "c":
			sendUsage := func() {
				cmd.client.Write([]byte("notice To create a public game specify whether it is public or private. When creating a private game, a password must also be provided."))
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

			g := newServerGame(<-s.newGameIDs)
			g.name = gameName
			g.password = gamePassword
			if !g.addClient(cmd.client) {
				log.Panicf("failed to add client to newly created game %+v %+v", g, cmd.client)
			}
			s.games = append(s.games, g) // TODO lock
		case bgammon.CommandJoin, "j":
			if clientGame != nil {
				cmd.client.sendEvent(&bgammon.EventFailedJoin{
					Reason: "Please leave the game you are in before joining another game.",
				})
				continue
			}

			sendUsage := func() {
				cmd.client.sendNotice("To join a public game specify its game ID. To join a private game, a password must also be specified.")
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
						cmd.client.sendEvent(&bgammon.EventFailedJoin{
							Reason: "Invalid password.",
						})
						continue COMMANDS
					}

					if !g.addClient(cmd.client) {
						cmd.client.sendEvent(&bgammon.EventFailedJoin{
							Reason: "Game is full.",
						})
					}
					continue COMMANDS
				}
			}
		case bgammon.CommandLeave, "l":
			if clientGame == nil {
				cmd.client.Write([]byte("failedleave You are not currently in a game."))
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
			cmd.client.Write(leftMessage)
			opponent := clientGame.opponent(cmd.client)
			if opponent != nil {
				opponent.Write(leftMessage)
			}
		case bgammon.CommandRoll, "r":
			if clientGame == nil {
				cmd.client.Write([]byte("notice You are not currently in a game."))
				continue
			}

			if !clientGame.roll(clientNumber) {
				cmd.client.Write([]byte("notice It is not your turn to roll."))
				continue
			}
			ev := &bgammon.EventRolled{
				Roll1: clientGame.Roll1,
				Roll2: clientGame.Roll2,
			}
			ev.Player = string(cmd.client.name)
			clientGame.eachClient(func(client *serverClient) {
				client.sendEvent(ev)
				if !client.json {
					clientGame.sendBoard(client)
				}
			})
			if clientGame.Turn == 0 && clientGame.Roll1 != 0 && clientGame.Roll2 != 0 {
				if clientGame.Roll1 > clientGame.Roll2 {
					clientGame.Turn = 1
				} else if clientGame.Roll2 > clientGame.Roll1 {
					clientGame.Turn = 2
				} else {
					clientGame.Roll1 = 0
					clientGame.Roll2 = 0
				}
			}
		case bgammon.CommandMove, "m", "mv":
			if clientGame == nil {
				cmd.client.Write([]byte("failedmove You are not currently in a game."))
				continue
			}

			if clientGame.Turn != clientNumber {
				cmd.client.Write([]byte("failedmove It is not your turn to move."))
				continue
			}

			sendUsage := func() {
				cmd.client.Write([]byte("failedmove Specify one or more moves in the form FROM/TO. For example: 8/4 6/4"))
			}

			if len(params) == 0 {
				sendUsage()
				continue
			}

			gameCopy := bgammon.Game{}
			gameCopy = *clientGame.Game
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

				originalFrom, originalTo := from, to
				from, to = bgammon.FlipSpace(from, clientNumber), bgammon.FlipSpace(to, clientNumber)

				legalMoves := gameCopy.LegalMoves()
				var found bool
				for j := range legalMoves {
					if legalMoves[j][0] == from && legalMoves[j][1] == to {
						found = true
						break
					}
				}
				if !found {
					log.Printf("available legal moves: %s", bgammon.FormatMoves(legalMoves, clientNumber))
					cmd.client.Write([]byte(fmt.Sprintf("failedmove %d/%d Illegal move.", originalFrom, originalTo)))
					continue COMMANDS
				}

				move := []int{from, to}
				moves = append(moves, move)
				gameCopy.Moves = append(gameCopy.Moves, move)
			}

			clientGame.Moves = gameCopy.Moves
			ev := &bgammon.EventMoved{
				Moves: moves,
			}
			ev.Player = string(cmd.client.name)
			clientGame.eachClient(func(client *serverClient) {
				client.sendEvent(ev)
				if !client.json {
					clientGame.sendBoard(client)
				}
			})
		case bgammon.CommandOk, "k":
			if clientGame == nil {
				cmd.client.Write([]byte("notice You are not currently in a game."))
				continue
			}

			legalMoves := clientGame.LegalMoves()
			if len(legalMoves) != 0 {
				playerNumber := 1
				if clientGame.client2 == cmd.client {
					playerNumber = 2
				}
				cmd.client.Write([]byte(fmt.Sprintf("failedok You still have the following legal moves available: %s", bgammon.FormatMoves(legalMoves, playerNumber))))
				continue
			}

			log.Println("legal to pass turn")
		case bgammon.CommandBoard, "b":
			if clientGame == nil {
				cmd.client.Write([]byte("notice You are not currently in a game."))
				continue
			}

			scanner := bufio.NewScanner(bytes.NewReader(clientGame.BoardState(clientNumber)))
			for scanner.Scan() {
				cmd.client.Write(append([]byte("notice "), scanner.Bytes()...))
			}
		case bgammon.CommandDisconnect:
			if clientGame != nil {
				clientGame.removeClient(cmd.client)
			}
			cmd.client.Terminate("Client disconnected")
		case bgammon.CommandPong:
			// Do nothing.
		default:
			log.Printf("unknown command %s", keyword)
		}
	}
}
