//go:build full

package server

import (
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
)

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

func (s *server) listenWebSocket(address string) {
	log.Printf("Listening for WebSocket connections on %s...", address)

	m := mux.NewRouter()
	m.HandleFunc("/reset/{id:[0-9]+}/{key:[A-Za-z0-9]+}", s.handleResetPassword)
	m.HandleFunc("/match/{id:[0-9]+}", s.handleMatch)
	m.HandleFunc("/matches", s.handleListMatches)
	m.HandleFunc("/leaderboard-casual-backgammon-single", s.handleLeaderboardFunc(matchTypeCasual, bgammon.VariantBackgammon, false))
	m.HandleFunc("/leaderboard-casual-backgammon-multi", s.handleLeaderboardFunc(matchTypeCasual, bgammon.VariantBackgammon, true))
	m.HandleFunc("/leaderboard-casual-acey-single", s.handleLeaderboardFunc(matchTypeCasual, bgammon.VariantAceyDeucey, false))
	m.HandleFunc("/leaderboard-casual-acey-multi", s.handleLeaderboardFunc(matchTypeCasual, bgammon.VariantAceyDeucey, true))
	m.HandleFunc("/leaderboard-casual-tabula-single", s.handleLeaderboardFunc(matchTypeCasual, bgammon.VariantTabula, false))
	m.HandleFunc("/leaderboard-casual-tabula-multi", s.handleLeaderboardFunc(matchTypeCasual, bgammon.VariantTabula, true))
	m.HandleFunc("/leaderboard-rated-backgammon-single", s.handleLeaderboardFunc(matchTypeRated, bgammon.VariantBackgammon, false))
	m.HandleFunc("/leaderboard-rated-backgammon-multi", s.handleLeaderboardFunc(matchTypeRated, bgammon.VariantBackgammon, true))
	m.HandleFunc("/leaderboard-rated-acey-single", s.handleLeaderboardFunc(matchTypeRated, bgammon.VariantAceyDeucey, false))
	m.HandleFunc("/leaderboard-rated-acey-multi", s.handleLeaderboardFunc(matchTypeRated, bgammon.VariantAceyDeucey, true))
	m.HandleFunc("/leaderboard-rated-tabula-single", s.handleLeaderboardFunc(matchTypeRated, bgammon.VariantTabula, false))
	m.HandleFunc("/leaderboard-rated-tabula-multi", s.handleLeaderboardFunc(matchTypeRated, bgammon.VariantTabula, true))
	m.HandleFunc("/stats", s.handleStatsFunc(0))
	m.HandleFunc("/stats-month", s.handleStatsFunc(1))
	m.HandleFunc("/stats-total", s.handleStatsFunc(2))
	m.HandleFunc("/stats-tabula", s.handleStatsFunc(3))
	m.HandleFunc("/stats-wildbg", s.handleStatsFunc(4))
	m.HandleFunc("/", s.handleWebSocket)

	err := http.ListenAndServe(address, m)
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
			log.Fatalf("failed to fetch serialize server statistics: %s", err)
		}
	case 3:
		stats, err := botStats("BOT_tabula", s.tz)
		if err != nil {
			log.Fatalf("failed to fetch tabula statistics: %s", err)
		}
		s.statsCache[statsType], err = json.Marshal(stats)
		if err != nil {
			log.Fatalf("failed to fetch serialize tabula statistics: %s", err)
		}
	default:
		stats, err := botStats("BOT_wildbg", s.tz)
		if err != nil {
			log.Fatalf("failed to fetch wildbg statistics: %s", err)
		}
		s.statsCache[statsType], err = json.Marshal(stats)
		if err != nil {
			log.Fatalf("failed to fetch serialize wildbg statistics: %s", err)
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
