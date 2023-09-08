package main

import (
	"bytes"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
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

	gamesLock   sync.RWMutex
	clientsLock sync.Mutex
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
	go s.handleTerminatedGames()
	return s
}

func (s *server) listen(network string, address string) {
	log.Printf("Listening for %s connections on %s...", strings.ToUpper(network), address)
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

func (s *server) addClient(c *serverClient) {
	s.clientsLock.Lock()
	defer s.clientsLock.Unlock()

	s.clients = append(s.clients, c)
}

func (s *server) removeClient(c *serverClient) {
	go func() {
		g := s.gameByClient(c)
		if g != nil {
			g.removeClient(c)
		}
		c.Terminate("")
	}()

	s.clientsLock.Lock()
	defer s.clientsLock.Unlock()

	for i, sc := range s.clients {
		if sc == c {
			s.clients = append(s.clients[:i], s.clients[i+1:]...)
			return
		}
	}
}

func (s *server) handleTerminatedGames() {
	t := time.NewTicker(time.Minute)
	for range t.C {
		s.gamesLock.Lock()

		i := 0
		for _, g := range s.games {
			if !g.terminated() {
				s.games[i] = g
				i++
			}
		}
		for j := i; j < len(s.games); j++ {
			s.games[j] = nil // Allow memory to be deallocated.
		}
		s.games = s.games[:i]

		s.gamesLock.Unlock()
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
	s.sendHello(c)
	s.addClient(c)

	go s.handlePingClient(c)
	go s.handleClientCommands(c)

	c.HandleReadWrite()

	// Remove client.
	s.removeClient(c)
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
	s.gamesLock.RLock()
	defer s.gamesLock.RUnlock()

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

		switch keyword {
		case bgammon.CommandHelp, "h":
			// TODO get extended help by specifying a command after help
			cmd.client.sendEvent(&bgammon.EventHelp{
				Topic:   "",
				Message: "Test help text",
			})
		case bgammon.CommandJSON:
			sendUsage := func() {
				cmd.client.sendNotice("To enable JSON formatted messages, send 'json on'. To disable JSON formatted messages, send 'json off'.")
			}
			if len(params) != 1 {
				sendUsage()
				continue
			}
			paramLower := strings.ToLower(string(params[0]))
			switch paramLower {
			case "on":
				cmd.client.json = true
				cmd.client.sendNotice("JSON formatted messages enabled.")
			case "off":
				cmd.client.json = false
				cmd.client.sendNotice("JSON formatted messages disabled.")
			default:
				sendUsage()
			}
		case bgammon.CommandSay, "s":
			if len(params) == 0 {
				continue
			}
			if clientGame == nil {
				cmd.client.sendNotice("Message not sent. You are not currently in a game.")
				continue
			}
			opponent := clientGame.opponent(cmd.client)
			if opponent == nil {
				cmd.client.sendNotice("Message not sent. There is no one else in the game.")
				continue
			}
			ev := &bgammon.EventSay{
				Message: string(bytes.Join(params, []byte(" "))),
			}
			ev.Player = string(cmd.client.name)
			opponent.sendEvent(ev)
		case bgammon.CommandList, "ls":
			ev := &bgammon.EventList{}

			s.gamesLock.RLock()
			for _, g := range s.games {
				if g.terminated() {
					continue
				}
				ev.Games = append(ev.Games, bgammon.GameListing{
					ID:       g.id,
					Password: len(g.password) != 0,
					Players:  g.playerCount(),
					Name:     string(g.name),
				})
			}
			s.gamesLock.RUnlock()

			cmd.client.sendEvent(ev)
		case bgammon.CommandCreate, "c":
			sendUsage := func() {
				cmd.client.sendNotice("To create a public game specify whether it is public or private. When creating a private game, a password must also be provided.")
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

			// Set default game name.
			if len(bytes.TrimSpace(gameName)) == 0 {
				abbr := "'s"
				lastLetter := cmd.client.name[len(cmd.client.name)-1]
				if lastLetter == 's' || lastLetter == 'S' {
					abbr = "'"
				}
				gameName = []byte(fmt.Sprintf("%s%s game", cmd.client.name, abbr))
			}

			g := newServerGame(<-s.newGameIDs)
			g.name = gameName
			g.password = gamePassword
			if !g.addClient(cmd.client) {
				log.Panicf("failed to add client to newly created game %+v %+v", g, cmd.client)
			}

			s.gamesLock.Lock()
			s.games = append(s.games, g)
			s.gamesLock.Unlock()
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

			s.gamesLock.Lock()
			for _, g := range s.games {
				if g.terminated() {
					continue
				}
				if g.id == gameID {
					if len(g.password) != 0 && (len(params) < 2 || !bytes.Equal(g.password, bytes.Join(params[2:], []byte(" ")))) {
						cmd.client.sendEvent(&bgammon.EventFailedJoin{
							Reason: "Invalid password.",
						})
						s.gamesLock.Unlock()
						continue COMMANDS
					}

					if !g.addClient(cmd.client) {
						cmd.client.sendEvent(&bgammon.EventFailedJoin{
							Reason: "Game is full.",
						})
					}
					s.gamesLock.Unlock()
					continue COMMANDS
				}
			}
			s.gamesLock.Unlock()
		case bgammon.CommandLeave, "l":
			if clientGame == nil {
				cmd.client.sendEvent(&bgammon.EventFailedLeave{
					Reason: "You are not currently in a game.",
				})
				continue
			}

			clientGame.removeClient(cmd.client)
		case bgammon.CommandRoll, "r":
			if clientGame == nil {
				cmd.client.sendEvent(&bgammon.EventFailedRoll{
					Reason: "You are not currently in a game.",
				})
				continue
			}

			if !clientGame.roll(cmd.client.playerNumber) {
				cmd.client.sendEvent(&bgammon.EventFailedRoll{
					Reason: "It is not your turn to roll.",
				})
				continue
			}

			ev := &bgammon.EventRolled{
				Roll1: clientGame.Roll1,
				Roll2: clientGame.Roll2,
			}
			ev.Player = string(cmd.client.name)
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
			clientGame.eachClient(func(client *serverClient) {
				client.sendEvent(ev)
				if clientGame.Turn != 0 || !client.json {
					clientGame.sendBoard(client)
				}
			})
		case bgammon.CommandMove, "m", "mv":
			if clientGame == nil {
				cmd.client.sendEvent(&bgammon.EventFailedMove{
					Reason: "You are not currently in a game.",
				})
				continue
			}

			if clientGame.Turn != cmd.client.playerNumber {
				cmd.client.sendEvent(&bgammon.EventFailedMove{
					Reason: "It is not your turn to move.",
				})
				continue
			}

			sendUsage := func() {
				cmd.client.sendEvent(&bgammon.EventFailedMove{
					Reason: "Specify one or more moves in the form FROM/TO. For example: 8/4 6/4",
				})
			}

			if len(params) == 0 {
				sendUsage()
				continue
			}

			var moves [][]int
			for i := range params {
				split := bytes.Split(params[i], []byte("/"))
				if len(split) != 2 {
					sendUsage()
					continue COMMANDS
				}
				from := bgammon.ParseSpace(string(split[0]))
				if from == -1 {
					sendUsage()
					continue COMMANDS
				}
				to := bgammon.ParseSpace(string(split[1]))
				if to == -1 {
					sendUsage()
					continue COMMANDS
				}

				if !bgammon.ValidSpace(from) || !bgammon.ValidSpace(to) {
					cmd.client.sendEvent(&bgammon.EventFailedMove{
						From:   from,
						To:     to,
						Reason: "Illegal move.",
					})
					continue COMMANDS
				}

				originalFrom, originalTo := from, to
				from, to = bgammon.FlipSpace(from, cmd.client.playerNumber), bgammon.FlipSpace(to, cmd.client.playerNumber)
				log.Printf("translated player %d %d-%d as %d-%d", cmd.client.playerNumber, originalFrom, originalTo, from, to)

				moves = append(moves, []int{from, to})
			}

			if !clientGame.AddMoves(moves) {
				cmd.client.sendEvent(&bgammon.EventFailedMove{
					From:   0,
					To:     0,
					Reason: "Illegal move.",
				})
				continue
			}

			var winEvent *bgammon.EventWin
			if clientGame.Winner != 0 {
				winEvent = &bgammon.EventWin{}
				if clientGame.Winner == 1 {
					winEvent.Player = clientGame.Player1.Name
				} else {
					winEvent.Player = clientGame.Player2.Name
				}
			}

			clientGame.eachClient(func(client *serverClient) {
				ev := &bgammon.EventMoved{
					Moves: bgammon.FlipMoves(moves, client.playerNumber),
				}
				ev.Player = string(cmd.client.name)
				client.sendEvent(ev)

				clientGame.sendBoard(client)

				if winEvent != nil {
					client.sendEvent(winEvent)
				}
			})
		case bgammon.CommandReset:
			if clientGame == nil {
				cmd.client.sendNotice("You are not currently in a game.")
				continue
			}

			if clientGame.Turn != cmd.client.playerNumber {
				cmd.client.sendNotice("It is not your turn.")
				continue
			}

			if len(clientGame.Moves) == 0 {
				continue
			}

			l := len(clientGame.Moves)
			undoMoves := make([][]int, l)
			for i, move := range clientGame.Moves {
				undoMoves[l-1-i] = []int{move[1], move[0]}
			}
			if !clientGame.AddMoves(undoMoves) {
				cmd.client.sendNotice("Failed to undo move: invalid move.")
			} else {
				clientGame.eachClient(func(client *serverClient) {
					ev := &bgammon.EventMoved{
						Moves: bgammon.FlipMoves(undoMoves, client.playerNumber),
					}
					ev.Player = string(cmd.client.name)

					client.sendEvent(ev)
					clientGame.sendBoard(client)
				})
			}
		case bgammon.CommandOk, "k":
			if clientGame == nil {
				cmd.client.sendNotice("You are not currently in a game.")
				continue
			}

			legalMoves := clientGame.LegalMoves()
			if len(legalMoves) != 0 {
				available := bgammon.FlipMoves(legalMoves, cmd.client.playerNumber)
				bgammon.SortMoves(available)
				cmd.client.sendEvent(&bgammon.EventFailedOk{
					Reason: fmt.Sprintf("The following legal moves are available: %s", bgammon.FormatMoves(available)),
				})
				continue
			}

			clientGame.NextTurn()
			clientGame.eachClient(func(client *serverClient) {
				clientGame.sendBoard(client)
			})
		case bgammon.CommandBoard, "b":
			if clientGame == nil {
				cmd.client.sendNotice("You are not currently in a game.")
				continue
			}

			clientGame.sendBoard(cmd.client)
		case bgammon.CommandDisconnect:
			if clientGame != nil {
				clientGame.removeClient(cmd.client)
			}
			cmd.client.Terminate("Client disconnected")
		case bgammon.CommandPong:
			// Do nothing.

			// TODO remove
		case "endgame":
			if clientGame == nil {
				cmd.client.sendNotice("You are not currently in a game.")
				continue
			}

			clientGame.Board = []int{0, 5, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, -5, 0, 0, 0}

			clientGame.eachClient(func(client *serverClient) {
				clientGame.sendBoard(client)
			})
		default:
			log.Printf("unknown command %s", keyword)
		}
	}
}
