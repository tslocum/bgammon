package server

const (
	matchTypeCasual = iota
	matchTypeRated
)

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

type botStatsEntry struct {
	Date    string
	Percent float64
	Wins    int
	Losses  int
}

type botStatsResult struct {
	History []*botStatsEntry
}
