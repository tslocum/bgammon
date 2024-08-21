package server

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
