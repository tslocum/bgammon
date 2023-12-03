package server

type serverStatsEntry struct {
	Date  string
	Games int
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
