package server

import "bytes"

const (
	matchTypeCasual = iota
	matchTypeRated
)

type account struct {
	id       int
	email    []byte
	username []byte
	password []byte

	follows []int

	icon  int
	icons []byte

	achievementIDs   []int
	achievementGames []int
	achievementDates []int64

	autoplay      bool
	highlight     bool
	pips          bool
	moves         bool
	flip          bool
	traditional   bool
	advanced      bool
	muteJoinLeave bool
	muteChat      bool
	muteRoll      bool
	muteMove      bool
	muteBearOff   bool
	speed         int8

	casual      *clientRating
	competitive *clientRating
}

type accountData struct {
	autoplay      int
	highlight     int
	pips          int
	moves         int
	flip          int
	traditional   int
	advanced      int
	muteJoinLeave int
	muteChat      int
	muteRoll      int
	muteMove      int
	muteBearOff   int
	achievements  []byte
}

func (a *account) load(d *accountData) {
	a.autoplay = d.autoplay == 1
	a.highlight = d.highlight == 1
	a.pips = d.pips == 1
	a.moves = d.moves == 1
	a.flip = d.flip == 1
	a.traditional = d.traditional == 1
	a.advanced = d.advanced == 1
	a.muteJoinLeave = d.muteJoinLeave == 1
	a.muteChat = d.muteChat == 1
	a.muteRoll = d.muteRoll == 1
	a.muteMove = d.muteMove == 1
	a.muteBearOff = d.muteBearOff == 1

	if len(d.achievements) == 0 {
		return
	}
	for _, achievement := range bytes.Split(d.achievements, []byte(",")) {
		split := bytes.Split(achievement, []byte("-"))
		if len(split) != 3 {
			continue
		}
		a.achievementIDs = append(a.achievementIDs, parseInt(split[0]))
		a.achievementGames = append(a.achievementGames, parseInt(split[1]))
		a.achievementDates = append(a.achievementDates, parseInt64(split[2]))
	}
}

type leaderboardEntry struct {
	User    string
	Rating  int
	Percent float64
	Wins    int
	Losses  int
}

type leaderboardResult struct {
	Leaderboard []*leaderboardEntry
}

type serverStatsEntry struct {
	Date     string
	Games    int
	Accounts int
}

type serverStatsResult struct {
	History []*serverStatsEntry
}

type accountStatsEntry struct {
	Date    string
	Percent float64
	Wins    int
	Losses  int
}

type accountStatsResult struct {
	History []*accountStatsEntry
}

type achievementStatsEntry struct {
	ID          int
	Name        string
	Description string
	Achieved    int
}

type achievementStatsResult struct {
	Players      int
	Achievements []*achievementStatsEntry
}
