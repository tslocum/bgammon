package server

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"code.rocket9labs.com/tslocum/bgammon"
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

	statsCache     [4][]byte
	statsCacheTime time.Time
	statsCacheLock sync.Mutex

	leaderboardCache     [12][]byte
	leaderboardCacheTime time.Time
	leaderboardCacheLock sync.Mutex

	mailServer   string
	passwordSalt string
	resetSalt    string

	tz *time.Location

	relayChat bool // Chats are not relayed normally. This option is only used by local servers.
	verbose   bool
}

func NewServer(tz string, dataSource string, mailServer string, passwordSalt string, resetSalt string, relayChat bool, verbose bool, allowDebug bool) *server {
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
		verbose:      verbose,
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

	/*gm := bgammon.NewGame(bgammon.VariantBackgammon)
	gm.Turn = 1
	gm.Roll1 = 2
	gm.Roll2 = 3
	log.Println(gm.MayBearOff(1, false))
	gm.Player1.Entered = true
	gm.Player2.Entered = true
	log.Println(gm.Board)
	//ok, expanded := gm.AddMoves([][]int8{{3, 1}}, false)
	//log.Println(ok, expanded, "!")
	log.Println(gm.MayBearOff(1, false))
	gs := &bgammon.GameState{
		Game:         gm,
		PlayerNumber: 1,
		Available:    gm.LegalMoves(false),
	}
	log.Printf("%+v", gs)
	os.Exit(0)*/

	go s.handleNewGameIDs()
	go s.handleNewClientIDs()
	go s.handleCommands()
	go s.handleTerminatedGames()
	return s
}

func (s *server) ListenLocal() chan net.Conn {
	conns := make(chan net.Conn)
	go s.handleLocal(conns)
	return conns
}

func (s *server) handleLocal(conns chan net.Conn) {
	for {
		local, remote := net.Pipe()

		conns <- local
		go s.handleConnection(remote)
	}
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

func (s *server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	const bufferSize = 8
	commands := make(chan []byte, bufferSize)
	events := make(chan []byte, bufferSize)

	wsClient := newWebSocketClient(r, w, commands, events, s.verbose)
	if wsClient == nil {
		return
	}

	now := time.Now().Unix()

	c := &serverClient{
		id:        <-s.newClientIDs,
		account:   -1,
		connected: now,
		active:    now,
		commands:  commands,
		autoplay:  true,
		Client:    wsClient,
	}
	s.handleClient(c)
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
			} else if g.forefeit != 0 && g.Winner == 0 {
				g.Winner = 1
				if g.forefeit == 1 {
					g.Winner = 2
				}
				err := recordMatchResult(g.Game, matchTypeCasual, g.account1, g.account2)
				if err != nil {
					log.Fatalf("failed to record match result: %s", err)
				}
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
		id:        <-s.newClientIDs,
		account:   -1,
		connected: now,
		active:    now,
		commands:  commands,
		autoplay:  true,
		Client:    newSocketClient(conn, commands, events, s.verbose),
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

// Analyze returns match analysis information calculated by gnubg.
func (s *server) Analyze(g *bgammon.Game) {
	cmd := exec.Command("gnubg", "--tty")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Fatal(err)
	}
	err = cmd.Start()
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			log.Println("STDOUT", string(scanner.Bytes()))
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Println("STDERR", string(scanner.Bytes()))
		}
	}()

	stdin.Write([]byte(fmt.Sprintf("new game\nset board %s\nanalyze game\n", gnubgPosition(g))))

	time.Sleep(2 * time.Second)
	os.Exit(0)
}

func RandInt(max int) int {
	i, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		panic(err)
	}
	return int(i.Int64())
}

func gnubgPosition(g *bgammon.Game) string {
	var opponent int8 = 2
	start := 0
	end := 25
	boardStart := 1
	boardEnd := 24
	delta := 1
	playerBarSpace := bgammon.SpaceBarPlayer
	opponentBarSpace := bgammon.SpaceBarOpponent
	switch g.Turn {
	case 1:
	case 2:
		opponent = 1
		start = 25
		end = 0
		boardStart = 24
		boardEnd = 1
		delta = -1
		playerBarSpace = bgammon.SpaceBarOpponent
		opponentBarSpace = bgammon.SpaceBarPlayer
	default:
		log.Fatalf("failed to analyze game: zero turn")
	}

	var buf []byte
	for space := boardStart; space != end; space += delta {
		playerCheckers := bgammon.PlayerCheckers(g.Board[space], g.Turn)
		for i := int8(0); i < playerCheckers; i++ {
			buf = append(buf, '1')
		}
		buf = append(buf, '0')
	}
	playerCheckers := bgammon.PlayerCheckers(g.Board[playerBarSpace], g.Turn)
	for i := int8(0); i < playerCheckers; i++ {
		buf = append(buf, '1')
	}
	buf = append(buf, '0')

	for space := boardEnd; space != start; space -= delta {
		opponentCheckers := bgammon.PlayerCheckers(g.Board[space], opponent)
		for i := int8(0); i < opponentCheckers; i++ {
			buf = append(buf, '1')
		}
		buf = append(buf, '0')
	}
	opponentCheckers := bgammon.PlayerCheckers(g.Board[opponentBarSpace], opponent)
	for i := int8(0); i < opponentCheckers; i++ {
		buf = append(buf, '1')
	}
	buf = append(buf, '0')

	for i := len(buf); i < 80; i++ {
		buf = append(buf, '0')
	}

	var out []byte
	for i := 0; i < len(buf); i += 8 {
		s := reverseString(string(buf[i : i+8]))
		v, err := strconv.ParseUint(s, 2, 8)
		if err != nil {
			panic(err)
		}
		out = append(out, byte(v))
	}

	position := base64.StdEncoding.EncodeToString(out)
	if len(position) == 0 {
		return ""
	}
	for position[len(position)-1] == '=' {
		position = position[:len(position)-1]
	}
	return position
}

func reverseString(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

type ratingPlayer struct {
	r       float64
	rd      float64
	sigma   float64
	outcome float64
}

func (p ratingPlayer) R() float64 {
	return p.r
}

func (p ratingPlayer) RD() float64 {
	return p.rd
}

func (p ratingPlayer) Sigma() float64 {
	return p.sigma
}

func (p ratingPlayer) SJ() float64 {
	return p.outcome
}
