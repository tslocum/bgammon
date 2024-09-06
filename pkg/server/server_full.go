//go:build full

package server

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"code.rocket9labs.com/tslocum/bgammon"
	"github.com/gorilla/mux"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/crypto/sha3"
)

func (s *server) Listen(network string, address string) {
	if s.passwordSalt == "" || s.resetSalt == "" || s.ipSalt == "" {
		log.Fatal("error: password, reset and ip salts must be configured")
	}

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

func (s *server) addCORSHeader(f func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		f(w, r)
	}
}

func (s *server) listenWebSocket(address string) {
	log.Printf("Listening for WebSocket connections on %s...", address)

	m := mux.NewRouter()
	handle := func(path string, f func(http.ResponseWriter, *http.Request)) *mux.Route {
		return m.HandleFunc(path, s.addCORSHeader(f))
	}

	handle("/reset/{id:[0-9]+}/{key:[A-Za-z0-9]+}", s.handleResetPassword)
	handle("/match/{id:[0-9]+}", s.handleMatch)
	handle("/matches.json", s.handleListMatches)
	handle("/leaderboard-casual-backgammon-single.json", s.handleLeaderboardFunc(matchTypeCasual, bgammon.VariantBackgammon, false))
	handle("/leaderboard-casual-backgammon-multi.json", s.handleLeaderboardFunc(matchTypeCasual, bgammon.VariantBackgammon, true))
	handle("/leaderboard-casual-acey-single.json", s.handleLeaderboardFunc(matchTypeCasual, bgammon.VariantAceyDeucey, false))
	handle("/leaderboard-casual-acey-multi.json", s.handleLeaderboardFunc(matchTypeCasual, bgammon.VariantAceyDeucey, true))
	handle("/leaderboard-casual-tabula-single.json", s.handleLeaderboardFunc(matchTypeCasual, bgammon.VariantTabula, false))
	handle("/leaderboard-casual-tabula-multi.json", s.handleLeaderboardFunc(matchTypeCasual, bgammon.VariantTabula, true))
	handle("/leaderboard-rated-backgammon-single.json", s.handleLeaderboardFunc(matchTypeRated, bgammon.VariantBackgammon, false))
	handle("/leaderboard-rated-backgammon-multi.json", s.handleLeaderboardFunc(matchTypeRated, bgammon.VariantBackgammon, true))
	handle("/leaderboard-rated-acey-single.json", s.handleLeaderboardFunc(matchTypeRated, bgammon.VariantAceyDeucey, false))
	handle("/leaderboard-rated-acey-multi.json", s.handleLeaderboardFunc(matchTypeRated, bgammon.VariantAceyDeucey, true))
	handle("/leaderboard-rated-tabula-single.json", s.handleLeaderboardFunc(matchTypeRated, bgammon.VariantTabula, false))
	handle("/leaderboard-rated-tabula-multi.json", s.handleLeaderboardFunc(matchTypeRated, bgammon.VariantTabula, true))
	handle("/stats.json", s.handleStatsFunc(1))
	handle("/stats-day.json", s.handleStatsFunc(0))
	handle("/stats-total.json", s.handleStatsFunc(2))
	handle("/stats-tabula.json", s.handleStatsFunc(3))
	handle("/stats-wildbg.json", s.handleStatsFunc(4))
	handle("/stats/{username:[A-Za-z0-9_\\-]+}.json", s.handleAccountStatsFunc(matchTypeCasual, bgammon.VariantBackgammon))
	handle("/stats/{username:[A-Za-z0-9_\\-]+}/backgammon.json", s.handleAccountStatsFunc(matchTypeCasual, bgammon.VariantBackgammon))
	handle("/stats/{username:[A-Za-z0-9_\\-]+}/acey.json", s.handleAccountStatsFunc(matchTypeCasual, bgammon.VariantAceyDeucey))
	handle("/stats/{username:[A-Za-z0-9_\\-]+}/tabula.json", s.handleAccountStatsFunc(matchTypeCasual, bgammon.VariantTabula))
	handle("/", s.handleWebSocket)

	certManager := autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(s.certFolder),
		HostPolicy: autocert.HostWhitelist(s.certDomain),
		Email:      s.certEmail,
	}

	server := &http.Server{
		Addr:    address,
		Handler: m,
		TLSConfig: &tls.Config{
			GetCertificate: certManager.GetCertificate,
			MinVersion:     tls.VersionTLS12,
		},
	}

	go func() {
		err := http.ListenAndServe(s.certAddress, certManager.HTTPHandler(m))
		log.Fatalf("failed to listen on %s: %s", s.certAddress, err)
	}()

	err := server.ListenAndServeTLS("", "")
	log.Fatalf("failed to listen on %s: %s", address, err)
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
		language:  "bgammon-en",
		accountID: -1,
		connected: now,
		active:    now,
		commands:  commands,
		Client:    wsClient,
	}
	s.handleClient(c)
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
		if listing == nil || listing.Password {
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

func (s *server) cachedLeaderboard(matchType int, variant int8, multiPoint bool) []byte {
	s.leaderboardCacheLock.Lock()
	defer s.leaderboardCacheLock.Unlock()

	var i int
	switch matchType {
	case matchTypeCasual:
		if multiPoint {
			i = 1
		}
	case matchTypeRated:
		if !multiPoint {
			i = 2
		} else {
			i = 3
		}
	}
	switch variant {
	case bgammon.VariantAceyDeucey:
		i += 4
	case bgammon.VariantTabula:
		i += 8
	}

	if !s.leaderboardCacheTime[i].IsZero() && time.Since(s.leaderboardCacheTime[i]) < 5*time.Minute {
		return s.leaderboardCache[i]
	}
	s.leaderboardCacheTime[i] = time.Now()

	result, err := getLeaderboard(matchType, variant, multiPoint)
	if err != nil {
		log.Fatalf("failed to get leaderboard: %s", err)
	}
	s.leaderboardCache[i], err = json.Marshal(result)
	if err != nil {
		log.Fatalf("failed to marshal %+v: %s", result, err)
	}

	return s.leaderboardCache[i]
}

func (s *server) cachedStats(statsType int) []byte {
	s.statsCacheLock.Lock()
	defer s.statsCacheLock.Unlock()

	if !s.statsCacheTime[statsType].IsZero() && time.Since(s.statsCacheTime[statsType]) < 5*time.Minute {
		return s.statsCache[statsType]
	}
	s.statsCacheTime[statsType] = time.Now()

	switch statsType {
	case 0:
		stats, err := dailyStats(s.tz)
		if err != nil {
			log.Fatalf("failed to fetch server statistics: %s", err)
		}
		s.statsCache[statsType], err = json.Marshal(stats)
		if err != nil {
			log.Fatalf("failed to marshal %+v: %s", stats, err)
		}
	case 1:
		stats, err := monthlyStats(s.tz)
		if err != nil {
			log.Fatalf("failed to fetch server statistics: %s", err)
		}
		s.statsCache[statsType], err = json.Marshal(stats)
		if err != nil {
			log.Fatalf("failed to marshal %+v: %s", stats, err)
		}
	case 2:
		stats, err := cumulativeStats(s.tz)
		if err != nil {
			log.Fatalf("failed to fetch server statistics: %s", err)
		}
		s.statsCache[statsType], err = json.Marshal(stats)
		if err != nil {
			log.Fatalf("failed to serialize server statistics: %s", err)
		}
	case 3:
		stats, err := accountStats("BOT_tabula", matchTypeCasual, bgammon.VariantBackgammon, s.tz)
		if err != nil {
			log.Fatalf("failed to fetch tabula statistics: %s", err)
		}
		s.statsCache[statsType], err = json.Marshal(stats)
		if err != nil {
			log.Fatalf("failed to serialize tabula statistics: %s", err)
		}
	default:
		stats, err := accountStats("BOT_wildbg", matchTypeCasual, bgammon.VariantBackgammon, s.tz)
		if err != nil {
			log.Fatalf("failed to fetch wildbg statistics: %s", err)
		}
		s.statsCache[statsType], err = json.Marshal(stats)
		if err != nil {
			log.Fatalf("failed to serialize wildbg statistics: %s", err)
		}
	}

	return s.statsCache[statsType]
}

func (s *server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil || id <= 0 {
		return
	}
	key := vars["key"]

	username, newPassword, err := confirmResetAccount(s.resetSalt, s.passwordSalt, id, key)
	if err != nil {
		log.Printf("failed to reset password: %s", err)
	}

	w.Header().Set("Content-Type", "text/html")
	if err != nil || username == "" || newPassword == "" {
		w.Write([]byte(`<!DOCTYPE html><html><body><h1>Invalid or expired password reset link.</h1></body></html>`))
		return
	}
	w.Write([]byte(`<!DOCTYPE html><html><body><h1>Your bgammon.org password has been reset.</h1>Your username is <b>` + username + `</b><br><br>Your new password is <b>` + newPassword + `</b></body></html>`))
}

func (s *server) handleMatch(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil || id <= 0 {
		return
	}

	timestamp, player1, player2, replay, err := matchInfo(id)
	if err != nil || len(replay) == 0 {
		log.Printf("failed to retrieve match: %s", err)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%d_%s_%s.match"`, timestamp, player1, player2))
	w.Write(replay)
}

func (s *server) handleListMatches(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.cachedMatches())
}

func (s *server) handleAccountStatsFunc(matchType int, variant int8) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		username := strings.ToLower(strings.TrimSpace(vars["username"]))
		if username == "" {
			w.Write([]byte(`<!DOCTYPE html><html><body><h1>No account specified.</h1></body></html>`))
			return
		}
		if strings.HasPrefix(username, "guest_") {
			username = "Guest_" + username[6:]
		} else if strings.HasPrefix(username, "bot_") {
			username = "BOT_" + username[4:]
		}
		w.Header().Set("Content-Type", "application/json")
		stats, err := accountStats(username, matchType, variant, s.tz)
		if err != nil {
			log.Fatalf("failed to fetch account statistics: %s", err)
		}
		buf, err := json.Marshal(stats)
		if err != nil {
			log.Fatalf("failed to serialize account statistics: %s", err)
		}
		w.Write(buf)

	}
}

func (s *server) handleLeaderboardFunc(matchType int, variant int8, multiPoint bool) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(s.cachedLeaderboard(matchType, variant, multiPoint))
	}
}

func (s *server) handleStatsFunc(statsType int) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(s.cachedStats(statsType))
	}
}

func (s *server) handlePrintDailyStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
}

func (s *server) handlePrintStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.cachedStats(0))
}

func (s *server) handlePrintCumulativeStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.cachedStats(2))
}

func (s *server) handlePrintTabulaStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.cachedStats(3))
}

func (s *server) handlePrintWildBGStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.cachedStats(4))
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

	buf := []byte(address + s.ipSalt)
	h := make([]byte, 64)
	sha3.ShakeSum256(h, buf)
	return fmt.Sprintf("%x\n", h)
}
