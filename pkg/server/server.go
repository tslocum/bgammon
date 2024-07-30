package server

//go:generate xgotext -no-locations -default bgammon -in . -out locales

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"code.rocket9labs.com/tslocum/bgammon"
	"code.rocket9labs.com/tslocum/gotext"
	"golang.org/x/text/language"
)

const clientTimeout = 40 * time.Second

const inactiveLimit = 600 // 10 minutes.

var allowDebugCommands bool

var (
	onlyNumbers            = regexp.MustCompile(`^[0-9]+$`)
	guestName              = regexp.MustCompile(`^guest[0-9]+$`)
	alphaNumericUnderscore = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
)

//go:embed locales
var assetFS embed.FS

var englishIdentifier = []byte("en")

func init() {
	gotext.SetDomain("bgammon-en")
}

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

	statsCache     [5][]byte
	statsCacheTime [5]time.Time
	statsCacheLock sync.Mutex

	leaderboardCache     [12][]byte
	leaderboardCacheTime [12]time.Time
	leaderboardCacheLock sync.Mutex

	motd string

	sortedCommands []string

	mailServer   string
	passwordSalt string
	resetSalt    string

	tz            *time.Location
	languageTags  []language.Tag
	languageNames [][]byte

	relayChat bool // Chats are not relayed normally. This option is only used by local servers.
	verbose   bool

	shutdownTime   time.Time
	shutdownReason string
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
	s.loadLocales()

	for command := range bgammon.HelpText {
		s.sortedCommands = append(s.sortedCommands, command)
	}
	sort.Slice(s.sortedCommands, func(i, j int) bool { return s.sortedCommands[i] < s.sortedCommands[j] })

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
	go s.handleGames()
	return s
}

func (s *server) loadLocales() {
	entries, err := assetFS.ReadDir("locales")
	if err != nil {
		log.Fatalf("failed to list files in locales directory: %s", err)
	}

	var availableTags = []language.Tag{
		language.MustParse("en_US"),
	}
	var availableNames = [][]byte{
		[]byte("en"),
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		availableTags = append(availableTags, language.MustParse(entry.Name()))
		availableNames = append(availableNames, []byte(entry.Name()))

		b, err := assetFS.ReadFile(fmt.Sprintf("locales/%s/%s.po", entry.Name(), entry.Name()))
		if err != nil {
			log.Fatalf("failed to read locale %s: %s", entry.Name(), err)
		}

		po := gotext.NewPo()
		po.Parse(b)
		gotext.GetStorage().AddTranslator(fmt.Sprintf("bgammon-%s", entry.Name()), po)
	}
	s.languageTags = availableTags
	s.languageNames = availableNames
}

func (s *server) matchLanguage(identifier []byte) []byte {
	if len(identifier) == 0 {
		return englishIdentifier
	}

	tag, err := language.Parse(string(identifier))
	if err != nil {
		return englishIdentifier
	}
	var preferred = []language.Tag{tag}

	useLanguage, index, _ := language.NewMatcher(s.languageTags).Match(preferred...)
	useLanguageCode := useLanguage.String()
	if index < 0 || useLanguageCode == "" || strings.HasPrefix(useLanguageCode, "en") {
		return englishIdentifier
	}
	return s.languageNames[index]
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

func (s *server) handleGames() {
	t := time.NewTicker(time.Minute)
	for range t.C {
		s.gamesLock.Lock()

		i := 0
		for _, g := range s.games {
			if !g.PartialHandled() && g.Player1.Rating != 0 && g.Player2.Rating != 0 {
				partialTurn := g.PartialTurn()
				if partialTurn != 0 {
					total := g.PartialTime()
					switch partialTurn {
					case 1:
						total += g.Player1.Inactive
					case 2:
						total += g.Player2.Inactive
					}
					if total >= inactiveLimit {
						g.inactive = partialTurn
						g.SetPartialHandled(true)
						if !g.terminated() {
							var player *serverClient
							var opponent *serverClient
							switch partialTurn {
							case 1:
								player = g.client1
								opponent = g.client2
							case 2:
								player = g.client2
								opponent = g.client1
							}
							if player != nil {
								player.sendNotice("You have been inactive for more than ten minutes. If your opponent leaves the match they will receive a win.")
							}
							if opponent != nil {
								opponent.sendNotice("Your opponent has been inactive for more than ten minutes. You may continue playing or leave the match at any time and receive a win.")
							}
						}
					}
				}
			}

			if !g.terminated() {
				s.games[i] = g
				i++
			} else if g.Winner == 0 && (g.inactive != 0 || g.forefeit != 0) {
				if g.inactive != 0 {
					g.Winner = 1
					if g.inactive == 1 {
						g.Winner = 2
					}
				} else {
					g.Winner = 1
					if g.forefeit == 1 {
						g.Winner = 2
					}
				}

				g.addReplayHeader()
				opponent := 1
				if g.Winner == 1 {
					opponent = 2
				}
				g.replay = append(g.replay, []byte(fmt.Sprintf("%d t", opponent)))

				err := recordGameResult(g, 4, g.replay)
				if err != nil {
					log.Fatalf("failed to record game result: %s", err)
				}
				err = recordMatchResult(g, matchTypeCasual)
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
		language:  "bgammon-en",
		accountID: -1,
		connected: now,
		active:    now,
		commands:  commands,
		Client:    newSocketClient(conn, commands, events, s.verbose),
	}
	s.sendWelcome(c)
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

func (s *server) sendWelcome(c *serverClient) {
	if c.json {
		return
	}
	c.Write(s.welcome)
}

func (s *server) sendMOTD(c *serverClient) {
	motd := s.motd
	if motd == "" {
		motd = fmt.Sprintf(gotext.GetD(c.language, "Connect with other players and stay up to date on the latest changes. Visit %s"), "bgammon.org/community")
	}
	c.sendNotice(motd)
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

func (s *server) handleShutdown() {
	var mins time.Duration
	var minutes int
	t := time.NewTicker(time.Minute)
	for {
		mins = time.Until(s.shutdownTime)
		if mins > 0 {
			minutes = int(mins.Minutes()) + 1
		}

		s.clientsLock.Lock()
		for _, sc := range s.clients {
			switch minutes {
			case 0:
				sc.sendBroadcast(gotext.GetD(sc.language, "The server is shutting down. Reason:"))
			case 1:
				sc.sendBroadcast(gotext.GetD(sc.language, "The server is shutting down in 1 minute. Reason:"))
			default:
				sc.sendBroadcast(gotext.GetD(sc.language, "The server is shutting down in %d minutes. Reason:", minutes))
			}
			sc.sendBroadcast(s.shutdownReason)
			sc.sendBroadcast(gotext.GetD(sc.language, "Please finish your match as soon as possible."))
		}
		s.clientsLock.Unlock()

		<-t.C
	}
}

func (s *server) shutdown(delay time.Duration, reason string) {
	if !s.shutdownTime.IsZero() {
		return
	}
	s.shutdownTime = time.Now().Add(delay)
	s.shutdownReason = reason
	go s.handleShutdown()
}

func RandInt(max int) int {
	i, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		panic(err)
	}
	return int(i.Int64())
}

// add8 adds two int8 values without overflowing.
func add8(a int8, b int8) int8 {
	v := a
	for i := int8(0); i < b; i++ {
		a++
		if a < 0 {
			return 127
		}
		v++
	}
	return v
}

// mul8 multiplies two int8 values without overflowing.
func mul8(a int8, b int8) int8 {
	var v int8
	for i := int8(0); i < b; i++ {
		v = add8(v, a)
		if v == 127 {
			return v
		}
	}
	return v
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
