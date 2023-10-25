package main

import (
	"bytes"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"code.rocket9labs.com/tslocum/bgammon"
)

const clientTimeout = 40 * time.Second

var onlyNumbers = regexp.MustCompile(`^[0-9]+$`)

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

func (s *server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	const bufferSize = 8
	commands := make(chan []byte, bufferSize)
	events := make(chan []byte, bufferSize)

	wsClient := newWebSocketClient(r, w, commands, events)
	if wsClient == nil {
		return
	}

	now := time.Now().Unix()

	c := &serverClient{
		id:         <-s.newClientIDs,
		account:    -1,
		connected:  now,
		lastActive: now,
		commands:   commands,
		Client:     wsClient,
	}
	s.handleClient(c)
}

func (s *server) listenWebSocket(address string) {
	log.Printf("Listening for WebSocket connections on %s...", address)
	err := http.ListenAndServe(address, http.HandlerFunc(s.handleWebSocket))
	log.Fatalf("failed to listen on %s: %s", address, err)
}

func (s *server) listen(network string, address string) {
	if strings.ToLower(network) == "ws" {
		go s.listenWebSocket(address)
		return
	}

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

func (s *server) nameAvailable(username []byte) bool {
	lower := bytes.ToLower(username)
	for _, c := range s.clients {
		if bytes.Equal(bytes.ToLower(c.name), lower) {
			return false
		}
	}
	return true
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

func (s *server) handleClient(c *serverClient) {
	s.addClient(c)

	log.Printf("Client %s connected", c.label())

	go s.handlePingClient(c)
	go s.handleClientCommands(c)

	c.HandleReadWrite()

	// Remove client.
	s.removeClient(c)

	log.Printf("Client %s disconnected", c.label())
}

func (s *server) handleConnection(conn net.Conn) {
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
	s.handleClient(c)
}

func (s *server) handlePingClient(c *serverClient) {
	// TODO only ping when there is no recent activity
	t := time.NewTicker(30 * time.Second)
	for {
		<-t.C

		if c.Terminated() {
			t.Stop()
			return
		}

		if len(c.name) == 0 {
			c.Terminate("User did not send login command within 30 seconds.")
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
	for {
		i := 100 + rand.Intn(900)
		name := []byte(fmt.Sprintf("Guest%d", i))

		if s.nameAvailable(name) {
			return name
		}
	}
}

func (s *server) sendHello(c *serverClient) {
	c.Write([]byte("hello Welcome to bgammon.org! Please log in by sending the 'login' command. You may specify a username, otherwise you will be assigned a random username. If you specify a username, you may also specify a password. Have fun!"))
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
		} else if cmd.client.terminating || cmd.client.Terminated() {
			continue
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
		params := bytes.Fields(cmd.command[startParameters:])

		// Require users to send login command first.
		if cmd.client.account == -1 {
			if keyword == bgammon.CommandLogin || keyword == bgammon.CommandLoginJSON || keyword == "l" || keyword == "lj" {
				if keyword == bgammon.CommandLoginJSON || keyword == "lj" {
					cmd.client.json = true
				}

				s.clientsLock.Lock()

				var username []byte
				var password []byte
				readUsername := func() bool {
					if cmd.client.json {
						if len(params) > 1 {
							username = params[1]
						}
					} else {
						if len(params) > 0 {
							username = params[0]
						}
					}
					if len(bytes.TrimSpace(username)) == 0 {
						username = s.randomUsername()
					}
					if onlyNumbers.Match(username) {
						cmd.client.Terminate("Invalid username: must contain at least one non-numeric character.")
						return false
					} else if !s.nameAvailable(username) {
						cmd.client.Terminate("Username unavailable.")
						return false
					}
					return true
				}
				if !readUsername() {
					s.clientsLock.Unlock()
					continue
				}
				if len(params) > 2 {
					password = bytes.ReplaceAll(bytes.Join(params[2:], []byte(" ")), []byte("_"), []byte(" "))
				}

				s.clientsLock.Unlock()

				if len(password) > 0 {
					cmd.client.account = 1
				} else {
					cmd.client.account = 0
				}
				cmd.client.name = username

				cmd.client.sendEvent(&bgammon.EventWelcome{
					PlayerName: string(cmd.client.name),
					Clients:    len(s.clients),
					Games:      len(s.games),
				})

				log.Printf("Client %d logged in as %s", cmd.client.id, cmd.client.name)

				// Rejoin match in progress.
				s.gamesLock.RLock()
				for _, g := range s.games {
					if g.terminated() || g.Winner != 0 {
						continue
					}

					var rejoin bool
					if bytes.Equal(cmd.client.name, g.allowed1) {
						rejoin = g.rejoin1
					} else if bytes.Equal(cmd.client.name, g.allowed2) {
						rejoin = g.rejoin2
					}
					if rejoin {
						ok, _ := g.addClient(cmd.client)
						if ok {
							cmd.client.sendNotice(fmt.Sprintf("Rejoined match: %s", g.name))
						}
					}
				}
				s.gamesLock.RUnlock()
				continue
			}

			cmd.client.Terminate("You must login before using other commands.")
			continue
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
				cmd.client.sendNotice("Message not sent: You are not currently in a match.")
				continue
			}
			opponent := clientGame.opponent(cmd.client)
			if opponent == nil {
				cmd.client.sendNotice("Message not sent: There is no one else in the match.")
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
					Points:   g.Points,
					Password: len(g.password) != 0,
					Players:  g.playerCount(),
					Name:     string(g.name),
				})
			}
			s.gamesLock.RUnlock()

			cmd.client.sendEvent(ev)
		case bgammon.CommandCreate, "c":
			if clientGame != nil {
				cmd.client.sendNotice("Failed to create match: Please leave the match you are in before creating another.")
				continue
			}

			sendUsage := func() {
				cmd.client.sendNotice("To create a public match please specify whether it is public or private, and also specify how many points are needed to win the match. When creating a private match, a password must also be provided.")
			}
			if len(params) < 2 {
				sendUsage()
				continue
			}

			var gamePassword []byte
			gameType := bytes.ToLower(params[0])
			var gameName []byte
			var gamePoints []byte
			switch {
			case bytes.Equal(gameType, []byte("public")):
				gamePoints = params[1]
				if len(params) > 2 {
					gameName = bytes.Join(params[2:], []byte(" "))
				}
			case bytes.Equal(gameType, []byte("private")):
				if len(params) < 3 {
					sendUsage()
					continue
				}
				gamePassword = bytes.ReplaceAll(params[1], []byte("_"), []byte(" "))
				gamePoints = params[2]
				if len(params) > 3 {
					gameName = bytes.Join(params[3:], []byte(" "))
				}
			default:
				sendUsage()
				continue
			}

			points, err := strconv.Atoi(string(gamePoints))
			if err != nil || points < 1 || points > 99 {
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
				gameName = []byte(fmt.Sprintf("%s%s match", cmd.client.name, abbr))
			}

			g := newServerGame(<-s.newGameIDs)
			g.name = gameName
			g.Points = points
			g.password = gamePassword
			ok, reason := g.addClient(cmd.client)
			if !ok {
				log.Panicf("failed to add client to newly created game %+v %+v: %s", g, cmd.client, reason)
			}

			s.gamesLock.Lock()
			s.games = append(s.games, g)
			s.gamesLock.Unlock()

			cmd.client.sendNotice(fmt.Sprintf("Created match: %s", g.name))
		case bgammon.CommandJoin, "j":
			if clientGame != nil {
				cmd.client.sendEvent(&bgammon.EventFailedJoin{
					Reason: "Please leave the match you are in before joining another.",
				})
				continue
			}

			sendUsage := func() {
				cmd.client.sendNotice("To join a match please specify its ID or the name of a player in the match. To join a private match, a password must also be specified.")
			}

			if len(params) == 0 {
				sendUsage()
				continue
			}

			var joinGameID int
			if onlyNumbers.Match(params[0]) {
				gameID, err := strconv.Atoi(string(params[0]))
				if err == nil && gameID > 0 {
					joinGameID = gameID
				}

				if joinGameID == 0 {
					sendUsage()
					continue
				}
			} else {
				paramLower := bytes.ToLower(params[0])
				s.clientsLock.Lock()
				for _, sc := range s.clients {
					if bytes.Equal(paramLower, bytes.ToLower(sc.name)) {
						g := s.gameByClient(sc)
						if g != nil {
							joinGameID = g.id
						}
						break
					}
				}
				s.clientsLock.Unlock()

				if joinGameID == 0 {
					cmd.client.sendEvent(&bgammon.EventFailedJoin{
						Reason: "Match not found.",
					})
					continue
				}
			}

			s.gamesLock.Lock()
			for _, g := range s.games {
				if g.terminated() {
					continue
				}
				if g.id == joinGameID {
					providedPassword := bytes.ReplaceAll(bytes.Join(params[1:], []byte(" ")), []byte("_"), []byte(" "))
					if len(g.password) != 0 && (len(params) < 2 || !bytes.Equal(g.password, providedPassword)) {
						cmd.client.sendEvent(&bgammon.EventFailedJoin{
							Reason: "Invalid password.",
						})
						s.gamesLock.Unlock()
						continue COMMANDS
					}
					ok, reason := g.addClient(cmd.client)
					s.gamesLock.Unlock()

					if !ok {
						cmd.client.sendEvent(&bgammon.EventFailedJoin{
							Reason: reason,
						})
					} else {
						cmd.client.sendNotice(fmt.Sprintf("Joined match: %s", g.name))
					}
					continue COMMANDS
				}
			}
			s.gamesLock.Unlock()

			cmd.client.sendEvent(&bgammon.EventFailedJoin{
				Reason: "Match not found.",
			})
		case bgammon.CommandLeave, "l":
			if clientGame == nil {
				cmd.client.sendEvent(&bgammon.EventFailedLeave{
					Reason: "You are not currently in a match.",
				})
				continue
			}

			if cmd.client.playerNumber == 1 {
				clientGame.rejoin1 = false
			} else {
				clientGame.rejoin2 = false
			}

			clientGame.removeClient(cmd.client)
		case bgammon.CommandDouble, "d":
			if clientGame == nil {
				cmd.client.sendNotice("You are not currently in a match.")
				continue
			}

			if clientGame.Turn != cmd.client.playerNumber {
				cmd.client.sendNotice("It is not your turn.")
				continue
			}

			gameState := &bgammon.GameState{
				Game:         clientGame.Game,
				PlayerNumber: cmd.client.playerNumber,
				Available:    clientGame.LegalMoves(),
			}
			if !gameState.MayDouble() {
				cmd.client.sendNotice("You may not double at this time.")
				continue
			}

			if clientGame.DoublePlayer != 0 && clientGame.DoublePlayer != cmd.client.playerNumber {
				cmd.client.sendNotice("You do not currently hold the doubling cube.")
				continue
			}

			clientGame.DoubleOffered = true

			cmd.client.sendNotice(fmt.Sprintf("Double offered to opponent (%d points).", clientGame.DoubleValue*2))
			clientGame.opponent(cmd.client).sendNotice(fmt.Sprintf("%s offers a double (%d points).", cmd.client.name, clientGame.DoubleValue*2))

			clientGame.eachClient(func(client *serverClient) {
				if client.json {
					clientGame.sendBoard(client)
				}
			})
		case bgammon.CommandResign:
			if clientGame == nil {
				cmd.client.sendNotice("You are not currently in a match.")
				continue
			}

			gameState := &bgammon.GameState{
				Game:         clientGame.Game,
				PlayerNumber: cmd.client.playerNumber,
				Available:    clientGame.LegalMoves(),
			}
			if !gameState.MayResign() {
				cmd.client.sendNotice("You may not resign at this time.")
				continue
			}

			cmd.client.sendNotice("Declined double offer")
			clientGame.opponent(cmd.client).sendNotice(fmt.Sprintf("%s declined double offer.", cmd.client.name))

			if cmd.client.playerNumber == 1 {
				clientGame.Player2.Points = clientGame.Player2.Points + clientGame.DoubleValue
				if clientGame.Player2.Points >= clientGame.Points {
					clientGame.Winner = 2
				} else {
					clientGame.Reset()
				}
			} else {
				clientGame.Player1.Points = clientGame.Player2.Points + clientGame.DoubleValue
				if clientGame.Player1.Points >= clientGame.Points {
					clientGame.Winner = 1
				} else {
					clientGame.Reset()
				}
			}

			var winEvent *bgammon.EventWin
			if clientGame.Winner != 0 {
				winEvent = &bgammon.EventWin{
					Points: clientGame.DoubleValue,
				}
				if clientGame.Winner == 1 {
					winEvent.Player = clientGame.Player1.Name
				} else {
					winEvent.Player = clientGame.Player2.Name
				}
			}
			clientGame.eachClient(func(client *serverClient) {
				clientGame.sendBoard(client)
				if winEvent != nil {
					client.sendEvent(winEvent)
				}
			})
		case bgammon.CommandRoll, "r":
			if clientGame == nil {
				cmd.client.sendEvent(&bgammon.EventFailedRoll{
					Reason: "You are not currently in a match.",
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
					Reason: "You are not currently in a match.",
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

				from, to = bgammon.FlipSpace(from, cmd.client.playerNumber), bgammon.FlipSpace(to, cmd.client.playerNumber)
				moves = append(moves, []int{from, to})
			}

			ok, expandedMoves := clientGame.AddMoves(moves)
			if !ok {
				cmd.client.sendEvent(&bgammon.EventFailedMove{
					From:   0,
					To:     0,
					Reason: "Illegal move.",
				})
				continue
			}

			var winEvent *bgammon.EventWin
			if clientGame.Winner != 0 {
				opponent := 1
				opponentHome := bgammon.SpaceHomePlayer
				if clientGame.Winner == 1 {
					opponent = 2
					opponentHome = bgammon.SpaceHomeOpponent
				}

				winPoints := 1
				if !bgammon.CanBearOff(clientGame.Board, opponent) {
					winPoints = 3 // Award backgammon.
				} else if clientGame.Board[opponentHome] == 0 {
					winPoints = 2 // Award gammon.
				}

				winEvent = &bgammon.EventWin{
					Points: winPoints * clientGame.DoubleValue,
				}
				if clientGame.Winner == 1 {
					winEvent.Player = clientGame.Player1.Name
					clientGame.Player1.Points = clientGame.Player1.Points + winPoints*clientGame.DoubleValue
					if clientGame.Player1.Points < clientGame.Points {
						clientGame.Reset()
					}
				} else {
					winEvent.Player = clientGame.Player2.Name
					clientGame.Player2.Points = clientGame.Player2.Points + winPoints*clientGame.DoubleValue
					if clientGame.Player2.Points < clientGame.Points {
						clientGame.Reset()
					}
				}
			}

			clientGame.eachClient(func(client *serverClient) {
				ev := &bgammon.EventMoved{
					Moves: bgammon.FlipMoves(expandedMoves, client.playerNumber),
				}
				ev.Player = string(cmd.client.name)
				client.sendEvent(ev)

				if winEvent != nil {
					client.sendEvent(winEvent)
				}

				clientGame.sendBoard(client)
			})
		case bgammon.CommandReset:
			if clientGame == nil {
				cmd.client.sendNotice("You are not currently in a match.")
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
			ok, _ := clientGame.AddMoves(undoMoves)
			if !ok {
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
				cmd.client.sendNotice("You are not currently in a match.")
				continue
			}

			if clientGame.DoubleOffered && clientGame.Turn != cmd.client.playerNumber {
				opponent := clientGame.opponent(cmd.client)
				if opponent == nil {
					cmd.client.sendNotice("You may not accept the double until your opponent rejoins the match.")
					continue
				}

				clientGame.DoubleOffered = false
				clientGame.DoubleValue = clientGame.DoubleValue * 2
				clientGame.DoublePlayer = cmd.client.playerNumber

				cmd.client.sendNotice("Accepted double.")
				opponent.sendNotice(fmt.Sprintf("%s accepted double.", cmd.client.name))

				clientGame.eachClient(func(client *serverClient) {
					clientGame.sendBoard(client)
				})
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
		case bgammon.CommandRematch, "rm":
			if clientGame == nil {
				cmd.client.sendNotice("You are not currently in a match.")
				continue
			} else if clientGame.Winner == 0 {
				cmd.client.sendNotice("The match you are in is still in progress.")
				continue
			} else if clientGame.rematch == cmd.client.playerNumber {
				cmd.client.sendNotice("You have already requested a rematch.")
				continue
			} else if clientGame.client1 == nil || clientGame.client2 == nil {
				cmd.client.sendNotice("Your opponent left the match.")
				continue
			} else if clientGame.rematch != 0 && clientGame.rematch != cmd.client.playerNumber {
				s.gamesLock.Lock()

				newGame := newServerGame(<-s.newGameIDs)
				newGame.name = clientGame.name
				newGame.password = clientGame.password
				newGame.client1 = clientGame.client1
				newGame.client2 = clientGame.client2
				newGame.Player1 = clientGame.Player1
				newGame.Player2 = clientGame.Player2
				s.games = append(s.games, newGame)

				clientGame.client1 = nil
				clientGame.client2 = nil

				s.gamesLock.Unlock()

				ev1 := &bgammon.EventJoined{
					GameID:       newGame.id,
					PlayerNumber: 1,
				}
				ev1.Player = newGame.Player1.Name

				ev2 := &bgammon.EventJoined{
					GameID:       newGame.id,
					PlayerNumber: 2,
				}
				ev2.Player = newGame.Player2.Name

				newGame.eachClient(func(client *serverClient) {
					client.sendEvent(ev1)
					client.sendEvent(ev2)
					newGame.sendBoard(client)
				})
			} else {
				clientGame.rematch = cmd.client.playerNumber

				clientGame.opponent(cmd.client).sendNotice("Your opponent would like to play again. Type /rematch to accept.")
				cmd.client.sendNotice("Rematch offer sent.")
				continue
			}
		case bgammon.CommandBoard, "b":
			if clientGame == nil {
				cmd.client.sendNotice("You are not currently in a match.")
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
				cmd.client.sendNotice("You are not currently in a match.")
				continue
			}

			clientGame.Turn = 1
			clientGame.Roll1 = 1
			clientGame.Roll2 = 2
			clientGame.Board = []int{0, 4, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, -4, 0, 0, 0}

			clientGame.eachClient(func(client *serverClient) {
				clientGame.sendBoard(client)
			})
		default:
			log.Printf("Received unknown command from client %s: %s", cmd.client.label(), cmd.command)
		}
	}
}
