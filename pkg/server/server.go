package server

//go:generate xgotext -no-locations -default bgammon -in . -out locales

import (
	"bytes"
	"crypto/rand"
	"embed"
	"fmt"
	"log"
	"math/big"
	"net"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"codeberg.org/tslocum/bgammon"
	"codeberg.org/tslocum/gotext"
	"golang.org/x/crypto/sha3"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

const clientTimeout = 40 * time.Second

const maxUsernameLength = 18

const inactiveLimit = 600 // 10 minutes.

var (
	anyNumbers             = regexp.MustCompile(`[0-9]+`)
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

	statsCache     [8][]byte
	statsCacheTime [8]time.Time
	statsCacheLock sync.Mutex

	leaderboardCache     [12][]byte
	leaderboardCacheTime [12]time.Time
	leaderboardCacheLock sync.Mutex

	diceStatsCache     []byte
	diceStatsCacheTime time.Time
	diceStatsCacheLock sync.Mutex

	defcon int

	motd string

	sortedCommands []string

	mailServer    string
	resetSalt     string
	passwordSalt  string
	ipAddressSalt string

	tz            *time.Location
	languageTags  []language.Tag
	languageNames [][]byte

	certFile string
	certKey  string

	relayChat bool // Chats are not relayed normally. This option is only used by local servers.
	verbose   bool
	debug     bool // Allow users to run debug commands.

	shutdownTime   time.Time
	shutdownReason string
}

type Options struct {
	TZ         string
	DataSource string
	MailServer string

	RelayChat bool
	Verbose   bool
	Debug     bool

	CertFile string
	CertKey  string

	ResetSalt     string
	PasswordSalt  string
	IPAddressSalt string
}

func NewServer(op *Options) *server {
	if op == nil {
		op = &Options{}
	}
	const bufferSize = 10
	s := &server{
		newGameIDs:    make(chan int),
		newClientIDs:  make(chan int),
		commands:      make(chan serverCommand, bufferSize),
		welcome:       []byte("hello Welcome to bgammon.org! Please log in by sending the 'login' command. You may specify a username, otherwise you will be assigned a random username. If you specify a username, you may also specify a password. Have fun!"),
		defcon:        5,
		mailServer:    op.MailServer,
		resetSalt:     op.ResetSalt,
		passwordSalt:  op.PasswordSalt,
		ipAddressSalt: op.IPAddressSalt,
		certFile:      op.CertFile,
		certKey:       op.CertKey,
		relayChat:     op.RelayChat,
		verbose:       op.Verbose,
		debug:         op.Debug,
	}
	s.loadLocales()

	for command := range bgammon.HelpText {
		s.sortedCommands = append(s.sortedCommands, command)
	}
	sort.Slice(s.sortedCommands, func(i, j int) bool { return s.sortedCommands[i] < s.sortedCommands[j] })

	if op.TZ != "" {
		var err error
		s.tz, err = time.LoadLocation(op.TZ)
		if err != nil {
			log.Fatalf("failed to parse timezone %s: %s", op.TZ, err)
		}
	} else {
		s.tz = time.UTC
	}

	if op.DataSource != "" {
		err := connectDB(op.DataSource)
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
		var tag language.Tag
		if !strings.ContainsRune(entry.Name(), '@') {
			tag = language.MustParse(entry.Name())
		}
		availableTags = append(availableTags, tag)
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

	for _, name := range s.languageNames {
		if bytes.Equal(name, identifier) {
			return identifier
		}
	}

	tag, err := language.Parse(string(identifier))
	if err != nil {
		return englishIdentifier
	}
	var preferred = []language.Tag{tag}

	useLanguage, index, _ := language.NewMatcher(s.languageTags).Match(preferred...)
	useLanguageCode := useLanguage.String()
	if index < 0 || useLanguageCode == "" {
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

	// Send followed player notifications.
	if c.accountID > 0 {
		for _, sc := range s.clients {
			if sc.accountID <= 0 {
				continue
			}
			for _, target := range sc.account.follows {
				if c.accountID == target {
					sc.sendNotice(fmt.Sprintf(gotext.GetD(c.language, "%s disconnected."), c.name))
				}
			}
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

			// Terminate completed matches after two minutes when only one client remains connected.
			if !g.terminated() && ((g.client1 != nil && g.client2 == nil) || (g.client1 == nil && g.client2 != nil)) {
				matchTimeout := (g.allowed1 != nil || g.allowed2 != nil) && time.Since(g.LastActive()) >= 15*time.Minute
				matchEnded := g.Ended != 0 && g.Winner != 0 && time.Now().Unix()-g.Ended >= 120
				if matchTimeout || matchEnded {
					var clients []*serverClient
					g.eachClient(func(client *serverClient) {
						clients = append(clients, client)
					})
					for _, sc := range clients {
						sc.sendNotice(gotext.GetD(sc.language, "Left completed match."))
						g.removeClient(sc)
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

				_, err := recordGameResult(g, 4, g.replay)
				if err != nil {
					log.Fatalf("failed to record game result: %s", err)
				}
				_, err = recordMatchResult(g, matchTypeCasual)
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

	sc := newSocketClient(conn, commands, events, s.verbose)
	sc.address = s.hashIP(conn.RemoteAddr().String())

	c := &serverClient{
		id:        <-s.newClientIDs,
		language:  "bgammon-en",
		accountID: -1,
		connected: now,
		active:    now,
		commands:  commands,
		Client:    sc,
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

func (s *server) sendMatchList(c *serverClient) {
	ev := &bgammon.EventList{}

	s.gamesLock.RLock()
	for _, g := range s.games {
		listing := g.listing(c.name)
		if listing == nil {
			continue
		}
		ev.Games = append(ev.Games, *listing)
	}
	s.gamesLock.RUnlock()

	c.sendEvent(ev)
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

func (s *server) hashIP(address string) string {
	leftBracket, rightBracket := strings.IndexByte(address, '['), strings.IndexByte(address, ']')
	if leftBracket != -1 && rightBracket != -1 && rightBracket > leftBracket {
		address = address[1:rightBracket]
	} else if strings.IndexByte(address, '.') != -1 {
		colon := strings.IndexByte(address, ':')
		if colon != -1 {
			address = address[:colon]
		}
	}

	buf := []byte(address + s.ipAddressSalt)
	h := make([]byte, 64)
	sha3.ShakeSum256(h, buf)
	return fmt.Sprintf("%x\n", h)
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
			if minutes == 0 {
				sc.sendBroadcast(gotext.GetD(sc.language, "The server is shutting down. Reason: %s", ""))
			} else {
				sc.sendBroadcast(gotext.GetND(sc.language, "The server is shutting down in %d minute. Reason:", "The server is shutting down in %d minutes. Reason:", minutes, minutes))
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

func DiceStats() string {
	var oneSame, doubles int
	var lastroll1, lastroll2 int
	var rolls [6]int

	const total = 10000000
	for i := 0; i < total; i++ {
		roll1 := RandInt(6) + 1
		roll2 := RandInt(6) + 1

		rolls[roll1-1]++
		rolls[roll2-1]++

		if roll1 == lastroll1 || roll1 == lastroll2 || roll2 == lastroll1 || roll2 == lastroll2 {
			oneSame++
		}

		if roll1 == roll2 {
			doubles++
		}

		lastroll1, lastroll2 = roll1, roll2
	}

	p := message.NewPrinter(language.English)
	return p.Sprintf("Rolled %d pairs of dice.\nDoubles: %d (%.0f%%). One same as last: %d (%.0f%%).\n1s: %d (%.0f%%), 2s: %d (%.0f%%), 3s: %d (%.0f%%), 4s: %d (%.0f%%), 5s: %d (%.0f%%), 6s: %d (%.0f%%).\n", total, doubles, float64(doubles)/float64(total)*100, oneSame, float64(oneSame)/float64(total)*100, rolls[0], float64(rolls[0])/float64(total*2)*100, rolls[1], float64(rolls[1])/float64(total*2)*100, rolls[2], float64(rolls[2])/float64(total*2)*100, rolls[3], float64(rolls[3])/float64(total*2)*100, rolls[4], float64(rolls[4])/float64(total*2)*100, rolls[5], float64(rolls[5])/float64(total*2)*100)
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

type gameCompat struct {
	bgammon.Game

	Started time.Time
	Ended   time.Time
}

type gameStateCompat struct {
	*gameCompat
	PlayerNumber int8
	Available    [][]int8 // Legal moves.
	Forced       bool     // A forced move is being played automatically.
	Spectating   bool
}

type eventBoardCompat struct {
	bgammon.Event
	gameStateCompat
}
