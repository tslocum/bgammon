package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"code.rocket9labs.com/tslocum/bgammon"
	"github.com/gorilla/mux"
)

func (s *server) listenWebSocket(address string) {
	log.Printf("Listening for WebSocket connections on %s...", address)

	m := mux.NewRouter()
	m.HandleFunc("/reset/{id:[0-9]+}/{key:[A-Za-z0-9]+}", s.handleResetPassword)
	m.HandleFunc("/match/{id:[0-9]+}", s.handleMatch)
	m.HandleFunc("/matches", s.handleListMatches)
	m.HandleFunc("/leaderboard-casual-backgammon-single", s.handleLeaderboardCasualBackgammonSingle)
	m.HandleFunc("/leaderboard-casual-backgammon-multi", s.handleLeaderboardCasualBackgammonMulti)
	m.HandleFunc("/leaderboard-casual-acey-single", s.handleLeaderboardCasualAceySingle)
	m.HandleFunc("/leaderboard-casual-acey-multi", s.handleLeaderboardCasualAceyMulti)
	m.HandleFunc("/leaderboard-rated-backgammon-single", s.handleLeaderboardRatedBackgammonSingle)
	m.HandleFunc("/leaderboard-rated-backgammon-multi", s.handleLeaderboardRatedBackgammonMulti)
	m.HandleFunc("/leaderboard-rated-acey-single", s.handleLeaderboardRatedAceySingle)
	m.HandleFunc("/leaderboard-rated-acey-multi", s.handleLeaderboardRatedAceyMulti)
	m.HandleFunc("/stats", s.handlePrintDailyStats)
	m.HandleFunc("/stats-total", s.handlePrintCumulativeStats)
	m.HandleFunc("/stats-tabula", s.handlePrintTabulaStats)
	m.HandleFunc("/stats-wildbg", s.handlePrintWildBGStats)
	m.HandleFunc("/", s.handleWebSocket)

	err := http.ListenAndServe(address, m)
	log.Fatalf("failed to listen on %s: %s", address, err)
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

func (s *server) cachedLeaderboard(matchType int, acey bool, multiPoint bool) []byte {
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
	if acey {
		i += 4
	}

	if time.Since(s.leaderboardCacheTime) < 5*time.Minute {
		return s.leaderboardCache[i]
	}
	s.leaderboardCacheTime = time.Now()

	for j := 0; j < 2; j++ {
		i := 0
		var acey bool
		if j == 1 {
			i += 4
			acey = true
		}
		result, err := getLeaderboard(matchTypeCasual, acey, false)
		if err != nil {
			log.Fatalf("failed to get leaderboard: %s", err)
		}
		s.leaderboardCache[i], err = json.Marshal(result)
		if err != nil {
			log.Fatalf("failed to marshal %+v: %s", result, err)
		}

		result, err = getLeaderboard(matchTypeCasual, acey, true)
		if err != nil {
			log.Fatalf("failed to get leaderboard: %s", err)
		}
		s.leaderboardCache[i+1], err = json.Marshal(result)
		if err != nil {
			log.Fatalf("failed to marshal %+v: %s", result, err)
		}

		result, err = getLeaderboard(matchTypeRated, acey, false)
		if err != nil {
			log.Fatalf("failed to get leaderboard: %s", err)
		}
		s.leaderboardCache[i+2], err = json.Marshal(result)
		if err != nil {
			log.Fatalf("failed to marshal %+v: %s", result, err)
		}

		result, err = getLeaderboard(matchTypeRated, acey, true)
		if err != nil {
			log.Fatalf("failed to get leaderboard: %s", err)
		}
		s.leaderboardCache[i+3], err = json.Marshal(result)
		if err != nil {
			log.Fatalf("failed to marshal %+v: %s", result, err)
		}
	}

	return s.leaderboardCache[i]
}

func (s *server) cachedStats(statsType int) []byte {
	s.statsCacheLock.Lock()
	defer s.statsCacheLock.Unlock()

	if time.Since(s.statsCacheTime) < 5*time.Minute {
		return s.statsCache[statsType]
	}
	s.statsCacheTime = time.Now()

	{
		stats, err := dailyStats(s.tz)
		if err != nil {
			log.Fatalf("failed to fetch server statistics: %s", err)
		}
		s.statsCache[0], err = json.Marshal(stats)
		if err != nil {
			log.Fatalf("failed to marshal %+v: %s", stats, err)
		}

		stats, err = cumulativeStats(s.tz)
		if err != nil {
			log.Fatalf("failed to fetch server statistics: %s", err)
		}
		s.statsCache[1], err = json.Marshal(stats)
		if err != nil {
			log.Fatalf("failed to fetch serialize server statistics: %s", err)
		}
	}

	{
		stats, err := botStats("BOT_tabula", s.tz)
		if err != nil {
			log.Fatalf("failed to fetch tabula statistics: %s", err)
		}
		s.statsCache[2], err = json.Marshal(stats)
		if err != nil {
			log.Fatalf("failed to fetch serialize tabula statistics: %s", err)
		}

		stats, err = botStats("BOT_wildbg", s.tz)
		if err != nil {
			log.Fatalf("failed to fetch wildbg statistics: %s", err)
		}
		s.statsCache[3], err = json.Marshal(stats)
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

func (s *server) handleLeaderboardCasualBackgammonSingle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.cachedLeaderboard(matchTypeCasual, false, false))
}

func (s *server) handleLeaderboardCasualBackgammonMulti(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.cachedLeaderboard(matchTypeCasual, false, true))
}

func (s *server) handleLeaderboardCasualAceySingle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.cachedLeaderboard(matchTypeCasual, true, false))
}

func (s *server) handleLeaderboardCasualAceyMulti(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.cachedLeaderboard(matchTypeCasual, true, true))
}

func (s *server) handleLeaderboardRatedBackgammonSingle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.cachedLeaderboard(matchTypeRated, false, false))
}

func (s *server) handleLeaderboardRatedBackgammonMulti(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.cachedLeaderboard(matchTypeRated, false, true))
}

func (s *server) handleLeaderboardRatedAceySingle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.cachedLeaderboard(matchTypeRated, true, false))
}

func (s *server) handleLeaderboardRatedAceyMulti(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.cachedLeaderboard(matchTypeRated, true, true))
}

func (s *server) handlePrintDailyStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.cachedStats(0))
}

func (s *server) handlePrintCumulativeStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.cachedStats(1))
}

func (s *server) handlePrintTabulaStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.cachedStats(2))
}

func (s *server) handlePrintWildBGStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(s.cachedStats(3))
}
