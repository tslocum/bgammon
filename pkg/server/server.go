package server

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"code.rocket9labs.com/tslocum/bgammon"
	"github.com/gorilla/mux"
)

const clientTimeout = 40 * time.Second

var allowDebugCommands bool

var (
	onlyNumbers            = regexp.MustCompile(`^[0-9]+$`)
	guestName              = regexp.MustCompile(`^guest[0-9]+$`)
	alphaNumericUnderscore = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
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
	welcome      []byte

	gamesLock   sync.RWMutex
	clientsLock sync.Mutex

	gamesCache     []byte
	gamesCacheTime time.Time
	gamesCacheLock sync.Mutex

	mailServer   string
	passwordSalt string
	resetSalt    string

	tz *time.Location

	relayChat bool // Chats are not relayed normally. This option is only used by local servers.
}

func NewServer(tz string, dataSource string, mailServer string, passwordSalt string, resetSalt string, relayChat bool, allowDebug bool) *server {
	const bufferSize = 10
	s := &server{
		newGameIDs:   make(chan int),
		newClientIDs: make(chan int),
		commands:     make(chan serverCommand, bufferSize),
		welcome:      []byte("hello Welcome to bgammon.org! Please log in by sending the 'login' command. You may specify a username, otherwise you will be assigned a random username. If you specify a username, you may also specify a password. Have fun!"),
		mailServer:   mailServer,
		passwordSalt: passwordSalt,
		resetSalt:    resetSalt,
		relayChat:    relayChat,
	}

	if tz != "" {
		var err error
		s.tz, err = time.LoadLocation(tz)
		if err != nil {
			log.Fatalf("failed to parse timezone %s: %s", tz, err)
		}
	} else {
		s.tz = time.UTC
	}

	if dataSource != "" {
		err := connectDB(dataSource)
		if err != nil {
			log.Fatalf("failed to connect to database: %s", err)
		}

		err = testDBConnection()
		if err != nil {
			log.Fatalf("failed to test database connection: %s", err)
		}

		initDB()

		log.Println("Connected to database successfully")
	}

	allowDebugCommands = allowDebug

	go s.handleNewGameIDs()
	go s.handleNewClientIDs()
	go s.handleCommands()
	go s.handleTerminatedGames()
	return s
}

func (s *server) cachedMatches() []byte {
	s.gamesCacheLock.Lock()
	defer s.gamesCacheLock.Unlock()

	if time.Since(s.gamesCacheTime) < 5*time.Second {
		return s.gamesCache
	}

	s.gamesLock.Lock()
	defer s.gamesLock.Unlock()

	var games []*bgammon.GameListing
	for _, g := range s.games {
		listing := g.listing(nil)
		if listing == nil || listing.Password || listing.Players == 2 {
			continue
		}
		games = append(games, listing)
	}

	s.gamesCacheTime = time.Now()
	if len(games) == 0 {
		s.gamesCache = []byte("[]")
		return s.gamesCache
	}
	var err error
	s.gamesCache, err = json.Marshal(games)
	if err != nil {
		log.Fatalf("failed to marshal %+v: %s", games, err)
	}
	return s.gamesCache
}

func (s *server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil || id <= 0 {
		return
	}
	key := vars["key"]

	newPassword, err := confirmResetAccount(s.resetSalt, s.passwordSalt, id, key)
	if err != nil {
		log.Printf("failed to reset password: %s", err)
	}

	w.Header().Set("Content-Type", "text/html")
	if err != nil || newPassword == "" {
		w.Write([]byte(`<!DOCTYPE html><html><body><h1>Invalid or expired password reset link.</h1></body></html>`))
		return
	}
	w.Write([]byte(`<!DOCTYPE html><html><body><h1>Your bgammon.org password has been reset.</h1>Your new password is <b>` + newPassword + `</b></body></html>`))
}

func (s *server) handleListMatches(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.cachedMatches())
}

func (s *server) handlePrintDailyStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	stats, err := dailyStats(s.tz)
	if err != nil {
		log.Fatalf("failed to fetch server statistics: %s", err)
	}
	buf, err := json.Marshal(stats)
	if err != nil {
		log.Fatalf("failed to fetch serialize server statistics: %s", err)
	}
	w.Write(buf)
}

func (s *server) handlePrintCumulativeStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	stats, err := cumulativeStats(s.tz)
	if err != nil {
		log.Fatalf("failed to fetch server statistics: %s", err)
	}
	buf, err := json.Marshal(stats)
	if err != nil {
		log.Fatalf("failed to fetch serialize server statistics: %s", err)
	}
	w.Write(buf)
}

func (s *server) handlePrintTabulaStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	stats, err := botStats("BOT_tabula", s.tz)
	if err != nil {
		log.Fatalf("failed to fetch tabula statistics: %s", err)
	}
	buf, err := json.Marshal(stats)
	if err != nil {
		log.Fatalf("failed to fetch serialize tabula statistics: %s", err)
	}
	w.Write(buf)
}

func (s *server) handlePrintWildBGStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	stats, err := botStats("BOT_wildbg", s.tz)
	if err != nil {
		log.Fatalf("failed to fetch wildbg statistics: %s", err)
	}
	buf, err := json.Marshal(stats)
	if err != nil {
		log.Fatalf("failed to fetch serialize wildbg statistics: %s", err)
	}
	w.Write(buf)
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

	m := mux.NewRouter()
	m.HandleFunc("/reset/{id:[0-9]+}/{key:[A-Za-z0-9]+}", s.handleResetPassword)
	m.HandleFunc("/matches", s.handleListMatches)
	m.HandleFunc("/stats", s.handlePrintDailyStats)
	m.HandleFunc("/stats-total", s.handlePrintCumulativeStats)
	m.HandleFunc("/stats-tabula", s.handlePrintTabulaStats)
	m.HandleFunc("/stats-wildbg", s.handlePrintWildBGStats)
	m.HandleFunc("/", s.handleWebSocket)

	err := http.ListenAndServe(address, m)
	log.Fatalf("failed to listen on %s: %s", address, err)
}

func (s *server) handleLocal(conns chan net.Conn) {
	for {
		local, remote := net.Pipe()

		conns <- local
		go s.handleConnection(remote)
	}
}

func (s *server) ListenLocal() chan net.Conn {
	conns := make(chan net.Conn)
	go s.handleLocal(conns)
	return conns
}

func (s *server) Listen(network string, address string) {
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

func (s *server) nameAllowed(username []byte) bool {
	return !guestName.Match(username)
}

func (s *server) clientByUsername(username []byte) *serverClient {
	lower := bytes.ToLower(username)
	for _, c := range s.clients {
		if bytes.Equal(bytes.ToLower(c.name), lower) {
			return c
		}
	}
	return nil
}

func (s *server) addClient(c *serverClient) {
	s.clientsLock.Lock()
	defer s.clientsLock.Unlock()

	s.clients = append(s.clients, c)
}

func (s *server) removeClient(c *serverClient) {
	g := s.gameByClient(c)
	if g != nil {
		g.removeClient(c)
	}
	c.Terminate("")

	close(c.commands)

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

// randomUsername returns a random guest username, and assumes clients are already locked.
func (s *server) randomUsername() []byte {
	for {
		name := []byte(fmt.Sprintf("Guest_%d", 100+RandInt(900)))

		if s.clientByUsername(name) == nil {
			return name
		}
	}
}

func (s *server) sendHello(c *serverClient) {
	if c.json {
		return
	}
	c.Write(s.welcome)
}

func (s *server) gameByClient(c *serverClient) *serverGame {
	s.gamesLock.RLock()
	defer s.gamesLock.RUnlock()

	for _, g := range s.games {
		if g.client1 == c || g.client2 == c {
			return g
		}
		for _, spec := range g.spectators {
			if spec == c {
				return g
			}
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
			resetCommand := keyword == bgammon.CommandResetPassword
			if resetCommand {
				if len(params) > 0 {
					email := bytes.ToLower(bytes.TrimSpace(params[0]))
					if len(email) > 0 {
						err := resetAccount(s.mailServer, s.resetSalt, email)
						if err != nil {
							log.Fatalf("failed to reset password: %s", err)
						}
					}
				}
				cmd.client.Terminate("resetpasswordok")
				continue
			}

			loginCommand := keyword == bgammon.CommandLogin || keyword == bgammon.CommandLoginJSON || keyword == "lj"
			registerCommand := keyword == bgammon.CommandRegister || keyword == bgammon.CommandRegisterJSON || keyword == "rj"
			if loginCommand || registerCommand {
				if keyword == bgammon.CommandLoginJSON || keyword == bgammon.CommandRegisterJSON || keyword == "lj" || keyword == "rj" {
					cmd.client.json = true
				}

				var username []byte
				var password []byte
				var randomUsername bool
				if registerCommand {
					sendUsage := func() {
						cmd.client.Terminate("Please enter an email, username and password.")
					}

					var email []byte
					if keyword == bgammon.CommandRegisterJSON || keyword == "rj" {
						if len(params) < 4 {
							sendUsage()
							continue
						}
						email = params[1]
						username = params[2]
						password = bytes.Join(params[3:], []byte("_"))
					} else {
						if len(params) < 3 {
							sendUsage()
							continue
						}
						email = params[0]
						username = params[1]
						password = bytes.Join(params[2:], []byte("_"))
					}
					if onlyNumbers.Match(username) {
						cmd.client.Terminate("Failed to register: Invalid username: must contain at least one non-numeric character.")
						continue
					}
					password = bytes.ReplaceAll(password, []byte(" "), []byte("_"))
					a := &account{
						email:    email,
						username: username,
						password: password,
					}
					err := registerAccount(s.passwordSalt, a)
					if err != nil {
						cmd.client.Terminate(fmt.Sprintf("Failed to register: %s", err))
						continue
					}
				} else {
					s.clientsLock.Lock()

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
							randomUsername = true
						} else if !alphaNumericUnderscore.Match(username) {
							cmd.client.Terminate("Invalid username: must contain only letters, numbers and underscores.")
							return false
						}
						if onlyNumbers.Match(username) {
							cmd.client.Terminate("Invalid username: must contain at least one non-numeric character.")
							return false
						} else if s.clientByUsername(username) != nil || s.clientByUsername(append([]byte("Guest_"), username...)) != nil || (!randomUsername && !s.nameAllowed(username)) {
							cmd.client.Terminate("That username is already in use.")
							return false
						}
						return true
					}
					if !readUsername() {
						s.clientsLock.Unlock()
						continue
					}
					if len(params) > 2 {
						password = bytes.ReplaceAll(bytes.Join(params[2:], []byte(" ")), []byte(" "), []byte("_"))
					}

					s.clientsLock.Unlock()
				}

				if len(password) > 0 {
					a, err := loginAccount(s.passwordSalt, username, password)
					if err != nil {
						cmd.client.Terminate(fmt.Sprintf("Failed to log in: %s", err))
						continue
					} else if a == nil {
						cmd.client.Terminate("No account was found with the provided username and password. To log in as a guest, do not enter a password.")
						continue
					}
					cmd.client.account = a.id
					cmd.client.name = a.username
					cmd.client.sendEvent(&bgammon.EventSettings{
						Highlight: a.highlight,
						Pips:      a.pips,
						Moves:     a.moves,
					})
				} else {
					cmd.client.account = 0
					if !randomUsername && !bytes.HasPrefix(username, []byte("BOT_")) && !bytes.HasPrefix(username, []byte("Guest_")) {
						username = append([]byte("Guest_"), username...)
					}
					cmd.client.name = username
				}

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
						g.addClient(cmd.client)
						cmd.client.sendNotice(fmt.Sprintf("Rejoined match: %s", g.name))
					}
				}
				s.gamesLock.RUnlock()
				continue
			}

			cmd.client.Terminate("You must login before using other commands.")
			continue
		}

		clientGame := s.gameByClient(cmd.client)
		if clientGame != nil && clientGame.client1 != cmd.client && clientGame.client2 != cmd.client {
			switch keyword {
			case bgammon.CommandHelp, "h", bgammon.CommandJSON, bgammon.CommandList, "ls", bgammon.CommandBoard, "b", bgammon.CommandLeave, "l", bgammon.CommandReplay, bgammon.CommandDisconnect, bgammon.CommandPong:
				// These commands are allowed to be used by spectators.
			default:
				cmd.client.sendNotice("Command ignored: You are spectating this match.")
				continue
			}
		}

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
			if s.relayChat {
				for _, spectator := range clientGame.spectators {
					spectator.sendEvent(ev)
				}
			}
		case bgammon.CommandList, "ls":
			ev := &bgammon.EventList{}

			s.gamesLock.RLock()
			for _, g := range s.games {
				listing := g.listing(cmd.client.name)
				if listing == nil {
					continue
				}
				ev.Games = append(ev.Games, *listing)
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

			var acey bool

			// Backwards-compatible acey-deucey parameter. Added in v1.1.5.
			noAcey := bytes.HasPrefix(gameName, []byte("0 ")) || bytes.Equal(gameName, []byte("0"))
			yesAcey := bytes.HasPrefix(gameName, []byte("1 ")) || bytes.Equal(gameName, []byte("1"))
			if noAcey || yesAcey {
				acey = yesAcey
				if len(gameName) > 1 {
					gameName = gameName[2:]
				} else {
					gameName = nil
				}
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

			g := newServerGame(<-s.newGameIDs, acey)
			g.name = gameName
			g.Points = points
			g.password = gamePassword
			g.addClient(cmd.client)

			s.gamesLock.Lock()
			s.games = append(s.games, g)
			s.gamesLock.Unlock()

			cmd.client.sendNotice(fmt.Sprintf("Created match: %s", g.name))

			if len(g.password) == 0 {
				cmd.client.sendNotice("Note: Please be patient as you wait for another player to join the match. A chime will sound when another player joins. While you wait, join the bgammon.org community via Discord, Matrix or IRC at bgammon.org/community")
			}
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
					spectator := g.addClient(cmd.client)
					s.gamesLock.Unlock()
					cmd.client.sendNotice(fmt.Sprintf("Joined match: %s", g.name))
					if spectator {
						cmd.client.sendNotice("You are spectating this match. Chat messages are not relayed.")
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
				Available:    clientGame.LegalMoves(false),
			}
			if !gameState.MayDouble() {
				cmd.client.sendNotice("You may not double at this time.")
				continue
			}

			if clientGame.DoublePlayer != 0 && clientGame.DoublePlayer != cmd.client.playerNumber {
				cmd.client.sendNotice("You do not currently hold the doubling cube.")
				continue
			}

			opponent := clientGame.opponent(cmd.client)
			if opponent == nil {
				cmd.client.sendNotice("You may not double until your opponent rejoins the match.")
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
				Available:    clientGame.LegalMoves(false),
			}
			if !gameState.MayResign() {
				cmd.client.sendNotice("You may not resign at this time.")
				continue
			}

			opponent := clientGame.opponent(cmd.client)
			if opponent == nil {
				cmd.client.sendNotice("You may not resign until your opponent rejoins the match.")
				continue
			}

			cmd.client.sendNotice("Declined double offer")
			clientGame.opponent(cmd.client).sendNotice(fmt.Sprintf("%s declined double offer.", cmd.client.name))

			acey := 0
			if clientGame.Acey {
				acey = 1
			}
			clientGame.replay = append([][]byte{[]byte(fmt.Sprintf("i %d %s %s %d %d %d %d %d %d", clientGame.Started.Unix(), clientGame.Player1.Name, clientGame.Player2.Name, clientGame.Points, clientGame.Player1.Points, clientGame.Player2.Points, clientGame.Winner, clientGame.DoubleValue, acey))}, clientGame.replay...)

			clientGame.replay = append(clientGame.replay, []byte(fmt.Sprintf("%d d %d 0", clientGame.Turn, clientGame.DoubleValue*2)))

			var reset bool
			if cmd.client.playerNumber == 1 {
				clientGame.Player2.Points = clientGame.Player2.Points + clientGame.DoubleValue
				if clientGame.Player2.Points >= clientGame.Points {
					clientGame.Winner = 2
					clientGame.Ended = time.Now()
				} else {
					reset = true
				}
			} else {
				clientGame.Player1.Points = clientGame.Player2.Points + clientGame.DoubleValue
				if clientGame.Player1.Points >= clientGame.Points {
					clientGame.Winner = 1
					clientGame.Ended = time.Now()
				} else {
					reset = true
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

				err := recordGameResult(clientGame.Game, 4, clientGame.client1.account, clientGame.client2.account, clientGame.replay)
				if err != nil {
					log.Fatalf("failed to record game result: %s", err)
				}
			}

			if reset {
				clientGame.Reset()
				clientGame.replay = clientGame.replay[:0]
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

			opponent := clientGame.opponent(cmd.client)
			if opponent == nil {
				cmd.client.sendEvent(&bgammon.EventFailedRoll{
					Reason: "You may not roll until your opponent rejoins the match.",
				})
				continue
			}

			if !clientGame.roll(cmd.client.playerNumber) {
				cmd.client.sendEvent(&bgammon.EventFailedRoll{
					Reason: "It is not your turn to roll.",
				})
				continue
			}

			clientGame.eachClient(func(client *serverClient) {
				ev := &bgammon.EventRolled{
					Roll1: clientGame.Roll1,
					Roll2: clientGame.Roll2,
				}
				ev.Player = string(cmd.client.name)
				if clientGame.Turn == 0 && client.playerNumber == 2 {
					ev.Roll1, ev.Roll2 = ev.Roll2, ev.Roll1
				}
				client.sendEvent(ev)
			})

			var skipBoard bool
			if clientGame.Turn == 0 && clientGame.Roll1 != 0 && clientGame.Roll2 != 0 {
				reroll := func() {
					clientGame.Roll1 = 0
					clientGame.Roll2 = 0
					if !clientGame.roll(clientGame.Turn) {
						log.Fatal("failed to re-roll while starting acey-deucey game")
					}

					ev := &bgammon.EventRolled{
						Roll1: clientGame.Roll1,
						Roll2: clientGame.Roll2,
					}
					ev.Player = string(clientGame.Player1.Name)
					if clientGame.Turn == 2 {
						ev.Player = string(clientGame.Player2.Name)
					}
					clientGame.eachClient(func(client *serverClient) {
						clientGame.sendBoard(client)
						client.sendEvent(ev)
					})
					skipBoard = true
				}

				if clientGame.Roll1 > clientGame.Roll2 {
					clientGame.Turn = 1
					if clientGame.Acey {
						reroll()
					}
				} else if clientGame.Roll2 > clientGame.Roll1 {
					clientGame.Turn = 2
					if clientGame.Acey {
						reroll()
					}
				} else {
					for {
						clientGame.Roll1 = 0
						clientGame.Roll2 = 0
						if !clientGame.roll(1) {
							log.Fatal("failed to re-roll to determine starting player")
						}
						if !clientGame.roll(2) {
							log.Fatal("failed to re-roll to determine starting player")
						}
						clientGame.eachClient(func(client *serverClient) {
							{
								ev := &bgammon.EventRolled{
									Roll1: clientGame.Roll1,
								}
								ev.Player = clientGame.Player1.Name
								if clientGame.Turn == 0 && client.playerNumber == 2 {
									ev.Roll1, ev.Roll2 = ev.Roll2, ev.Roll1
								}
								client.sendEvent(ev)
							}
							{
								ev := &bgammon.EventRolled{
									Roll1: clientGame.Roll1,
									Roll2: clientGame.Roll2,
								}
								ev.Player = clientGame.Player2.Name
								if clientGame.Turn == 0 && client.playerNumber == 2 {
									ev.Roll1, ev.Roll2 = ev.Roll2, ev.Roll1
								}
								client.sendEvent(ev)
							}
						})
						if clientGame.Roll1 > clientGame.Roll2 {
							clientGame.Turn = 1
							if clientGame.Acey {
								reroll()
							}
							break
						} else if clientGame.Roll2 > clientGame.Roll1 {
							clientGame.Turn = 2
							if clientGame.Acey {
								reroll()
							}
							break
						}
					}
				}
			}
			if !skipBoard {
				clientGame.eachClient(func(client *serverClient) {
					if clientGame.Turn != 0 || !client.json {
						clientGame.sendBoard(client)
					}
				})
			}
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

			opponent := clientGame.opponent(cmd.client)
			if opponent == nil {
				cmd.client.sendEvent(&bgammon.EventFailedMove{
					Reason: "You may not move until your opponent rejoins the match.",
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

			ok, expandedMoves := clientGame.AddMoves(moves, false)
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
				opponentEntered := clientGame.Player1.Entered
				playerBar := bgammon.SpaceBarPlayer
				if clientGame.Winner == 1 {
					opponent = 2
					opponentHome = bgammon.SpaceHomeOpponent
					opponentEntered = clientGame.Player2.Entered
					playerBar = bgammon.SpaceBarOpponent
				}

				backgammon := bgammon.PlayerCheckers(clientGame.Board[playerBar], opponent) != 0
				if !backgammon {
					homeStart, homeEnd := bgammon.HomeRange(clientGame.Winner)
					bgammon.IterateSpaces(homeStart, homeEnd, clientGame.Acey, func(space, spaceCount int) {
						if bgammon.PlayerCheckers(clientGame.Board[space], opponent) != 0 {
							backgammon = true
						}
					})
				}

				var winPoints int
				if !clientGame.Acey {
					if backgammon {
						winPoints = 3 // Award backgammon.
					} else if clientGame.Board[opponentHome] == 0 {
						winPoints = 2 // Award gammon.
					} else {
						winPoints = 1
					}
				} else {
					for space := 0; space < bgammon.BoardSpaces; space++ {
						if (space == bgammon.SpaceHomePlayer || space == bgammon.SpaceHomeOpponent) && opponentEntered {
							continue
						}
						winPoints += bgammon.PlayerCheckers(clientGame.Board[space], opponent)
					}
				}

				acey := 0
				if clientGame.Acey {
					acey = 1
				}
				clientGame.replay = append([][]byte{[]byte(fmt.Sprintf("i %d %s %s %d %d %d %d %d %d", clientGame.Started.Unix(), clientGame.Player1.Name, clientGame.Player2.Name, clientGame.Points, clientGame.Player1.Points, clientGame.Player2.Points, clientGame.Winner, winPoints, acey))}, clientGame.replay...)

				r1, r2 := clientGame.Roll1, clientGame.Roll2
				if r2 > r1 {
					r1, r2 = r2, r1
				}
				var movesFormatted []byte
				if len(clientGame.Moves) != 0 {
					movesFormatted = append([]byte(" "), bgammon.FormatMoves(clientGame.Moves)...)
				}
				clientGame.replay = append(clientGame.replay, []byte(fmt.Sprintf("%d r %d-%d%s", clientGame.Turn, r1, r2, movesFormatted)))

				winEvent = &bgammon.EventWin{
					Points: winPoints * clientGame.DoubleValue,
				}
				var reset bool
				if clientGame.Winner == 1 {
					winEvent.Player = clientGame.Player1.Name
					clientGame.Player1.Points = clientGame.Player1.Points + winPoints*clientGame.DoubleValue
					if clientGame.Player1.Points < clientGame.Points {
						reset = true
					} else {
						clientGame.Ended = time.Now()
					}
				} else {
					winEvent.Player = clientGame.Player2.Name
					clientGame.Player2.Points = clientGame.Player2.Points + winPoints*clientGame.DoubleValue
					if clientGame.Player2.Points < clientGame.Points {
						reset = true
					} else {
						clientGame.Ended = time.Now()
					}
				}

				winType := winPoints
				if clientGame.Acey {
					winType = 1
				}
				err := recordGameResult(clientGame.Game, winType, clientGame.client1.account, clientGame.client2.account, clientGame.replay)
				if err != nil {
					log.Fatalf("failed to record game result: %s", err)
				}

				if reset {
					clientGame.Reset()
					clientGame.replay = clientGame.replay[:0]
				}
			}

			clientGame.eachClient(func(client *serverClient) {
				ev := &bgammon.EventMoved{
					Moves: bgammon.FlipMoves(expandedMoves, client.playerNumber),
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
			ok, _ := clientGame.AddMoves(undoMoves, false)
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

			opponent := clientGame.opponent(cmd.client)
			if opponent == nil {
				cmd.client.sendNotice("You must wait until your opponent rejoins the match before continuing the game.")
				continue
			}

			if clientGame.DoubleOffered {
				if clientGame.Turn != cmd.client.playerNumber {
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

					clientGame.replay = append(clientGame.replay, []byte(fmt.Sprintf("%d d %d 1", clientGame.Turn, clientGame.DoubleValue)))
					clientGame.eachClient(func(client *serverClient) {
						clientGame.sendBoard(client)
					})
				} else {
					cmd.client.sendNotice("Waiting for response from opponent.")
				}
				continue
			} else if clientGame.Turn != cmd.client.playerNumber {
				cmd.client.sendNotice("It is not your turn.")
				continue
			}

			if clientGame.Roll1 == 0 || clientGame.Roll2 == 0 {
				cmd.client.sendNotice("You must roll first.")
				continue
			}

			legalMoves := clientGame.LegalMoves(false)
			if len(legalMoves) != 0 {
				available := bgammon.FlipMoves(legalMoves, cmd.client.playerNumber)
				bgammon.SortMoves(available)
				cmd.client.sendEvent(&bgammon.EventFailedOk{
					Reason: fmt.Sprintf("The following legal moves are available: %s", bgammon.FormatMoves(available)),
				})
				continue
			}

			recordEvent := func() {
				r1, r2 := clientGame.Roll1, clientGame.Roll2
				if r2 > r1 {
					r1, r2 = r2, r1
				}
				var movesFormatted []byte
				if len(clientGame.Moves) != 0 {
					movesFormatted = append([]byte(" "), bgammon.FormatMoves(clientGame.Moves)...)
				}
				clientGame.replay = append(clientGame.replay, []byte(fmt.Sprintf("%d r %d-%d%s", clientGame.Turn, r1, r2, movesFormatted)))
			}

			if clientGame.Acey && ((clientGame.Roll1 == 1 && clientGame.Roll2 == 2) || (clientGame.Roll1 == 2 && clientGame.Roll2 == 1)) && len(clientGame.Moves) == 2 {
				var doubles int
				if len(params) > 0 {
					doubles, _ = strconv.Atoi(string(params[0]))
				}
				if doubles < 1 || doubles > 6 {
					cmd.client.sendEvent(&bgammon.EventFailedOk{
						Reason: "Choose which doubles you want for your acey-deucey.",
					})
					continue
				}

				recordEvent()
				clientGame.NextTurn(true)
				clientGame.Roll1, clientGame.Roll2 = doubles, doubles
				clientGame.Reroll = true

				clientGame.eachClient(func(client *serverClient) {
					ev := &bgammon.EventRolled{
						Roll1:    clientGame.Roll1,
						Roll2:    clientGame.Roll2,
						Selected: true,
					}
					ev.Player = string(cmd.client.name)
					client.sendEvent(ev)
				})
			} else if clientGame.Acey && clientGame.Reroll {
				recordEvent()
				clientGame.NextTurn(true)
				clientGame.Roll1, clientGame.Roll2 = 0, 0
				if !clientGame.roll(cmd.client.playerNumber) {
					cmd.client.Terminate("Server error")
					opponent.Terminate("Server error")
					continue
				}
				clientGame.Reroll = false

				clientGame.eachClient(func(client *serverClient) {
					ev := &bgammon.EventRolled{
						Roll1: clientGame.Roll1,
						Roll2: clientGame.Roll2,
					}
					ev.Player = string(cmd.client.name)
					client.sendEvent(ev)
					clientGame.sendBoard(client)
				})
			} else {
				recordEvent()
				clientGame.NextTurn(false)
				if clientGame.Winner == 0 {
					gameState := &bgammon.GameState{
						Game:         clientGame.Game,
						PlayerNumber: clientGame.Turn,
						Available:    clientGame.LegalMoves(false),
					}
					if !gameState.MayDouble() {
						if !clientGame.roll(clientGame.Turn) {
							cmd.client.Terminate("Server error")
							opponent.Terminate("Server error")
							continue
						}
						clientGame.eachClient(func(client *serverClient) {
							ev := &bgammon.EventRolled{
								Roll1: clientGame.Roll1,
								Roll2: clientGame.Roll2,
							}
							if clientGame.Turn == 1 {
								ev.Player = gameState.Player1.Name
							} else {
								ev.Player = gameState.Player2.Name
							}
							client.sendEvent(ev)
						})
					}
				}
			}

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

				newGame := newServerGame(<-s.newGameIDs, clientGame.Acey)
				newGame.name = clientGame.name
				newGame.Points = clientGame.Points
				newGame.password = clientGame.password
				newGame.client1 = clientGame.client1
				newGame.client2 = clientGame.client2
				newGame.spectators = make([]*serverClient, len(clientGame.spectators))
				copy(newGame.spectators, clientGame.spectators)
				newGame.Player1 = clientGame.Player1
				newGame.Player2 = clientGame.Player2
				newGame.allowed1 = clientGame.allowed1
				newGame.allowed2 = clientGame.allowed2
				s.games = append(s.games, newGame)

				clientGame.client1 = nil
				clientGame.client2 = nil
				clientGame.spectators = nil

				s.gamesLock.Unlock()

				{
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
					newGame.client1.sendEvent(ev1)
					newGame.client1.sendEvent(ev2)
					newGame.sendBoard(newGame.client1)
				}

				{
					ev1 := &bgammon.EventJoined{
						GameID:       newGame.id,
						PlayerNumber: 1,
					}
					ev1.Player = newGame.Player2.Name
					ev2 := &bgammon.EventJoined{
						GameID:       newGame.id,
						PlayerNumber: 2,
					}
					ev2.Player = newGame.Player1.Name
					newGame.client2.sendEvent(ev1)
					newGame.client2.sendEvent(ev2)
					newGame.sendBoard(newGame.client2)
				}

				for _, spectator := range newGame.spectators {
					newGame.sendBoard(spectator)
				}
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
		case bgammon.CommandPassword:
			if cmd.client.account == 0 {
				cmd.client.sendNotice("Failed to change password: you are logged in as a guest.")
				continue
			} else if len(params) < 2 {
				cmd.client.sendNotice("Please specify your old and new passwords as follows: password <old> <new>")
				continue
			}

			a, err := loginAccount(s.passwordSalt, cmd.client.name, params[0])
			if err != nil || a == nil || a.id == 0 {
				cmd.client.sendNotice("Failed to change password: incorrect existing password.")
				continue
			}

			err = setAccountPassword(s.passwordSalt, a.id, string(bytes.Join(params[1:], []byte("_"))))
			if err != nil {
				cmd.client.sendNotice("Failed to change password.")
				continue
			}
			cmd.client.sendNotice("Password changed successfully.")
		case bgammon.CommandSet:
			if cmd.client.account == 0 {
				continue
			} else if len(params) < 2 {
				cmd.client.sendNotice("Please specify the setting name and value as follows: set <name> <value>")
				continue
			}

			name := string(bytes.ToLower(params[0]))
			if name != "highlight" && name != "pips" && name != "moves" {
				cmd.client.sendNotice("Please specify the setting name and value as follows: set <name> <value>")
				continue
			}

			value, err := strconv.Atoi(string(params[1]))
			if err != nil || value < 0 {
				cmd.client.sendNotice("Invalid setting value provided.")
				continue
			}
			_ = setAccountSetting(cmd.client.account, name, value)
		case bgammon.CommandReplay:
			var (
				id     int
				replay []byte
				err    error
			)
			if len(params) == 0 {
				if clientGame == nil || clientGame.Winner == 0 {
					cmd.client.sendNotice("Please specify the game as follows: replay <id>")
					continue
				}
				id = -1
				replay = bytes.Join(clientGame.replay, []byte("\n"))
			} else {
				id, err = strconv.Atoi(string(params[0]))
				if err != nil || id < 0 {
					cmd.client.sendNotice("Invalid replay ID provided.")
					continue
				}
				replay, err = replayByID(id)
				if err != nil {
					cmd.client.sendNotice("Invalid replay ID provided.")
					continue
				}
			}
			if len(replay) == 0 {
				cmd.client.sendNotice("No replay was recorded for that game.")
				continue
			}
			cmd.client.sendEvent(&bgammon.EventReplay{
				ID:      id,
				Content: replay,
			})
		case bgammon.CommandHistory:
			if len(params) == 0 {
				cmd.client.sendNotice("Please specify the player as follows: history <username>")
				continue
			}

			matches, err := matchHistory(string(params[0]))
			if err != nil {
				cmd.client.sendNotice("Invalid replay ID provided.")
				continue
			}
			ev := &bgammon.EventHistory{
				Matches: matches,
			}
			ev.Player = string(params[0])
			cmd.client.sendEvent(ev)
		case bgammon.CommandDisconnect:
			if clientGame != nil {
				clientGame.removeClient(cmd.client)
			}
			cmd.client.Terminate("Client disconnected")
		case bgammon.CommandPong:
			// Do nothing.
		case "endgame":
			if !allowDebugCommands {
				cmd.client.sendNotice("You are not allowed to use that command.")
				continue
			}

			if clientGame == nil {
				cmd.client.sendNotice("You are not currently in a match.")
				continue
			}

			clientGame.Turn = 1
			clientGame.Roll1 = 6
			clientGame.Roll2 = 6
			clientGame.Board = []int{1, 0, -2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0, -1, 1, -1}

			clientGame.eachClient(func(client *serverClient) {
				clientGame.sendBoard(client)
			})
		default:
			log.Printf("Received unknown command from client %s: %s", cmd.client.label(), cmd.command)
			cmd.client.sendNotice(fmt.Sprintf("Unknown command: %s", cmd.command))
		}
	}
}

func RandInt(max int) int {
	i, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		panic(err)
	}
	return int(i.Int64())
}
