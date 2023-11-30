package server

type serverStatsEntry struct {
	Date  string
	Games int
}

type serverStatsResult struct {
	History []*serverStatsEntry
}

type wildBGStatsEntry struct {
	Date    string
	Percent float64
	Wins    int
	Losses  int
}

type wildBGStatsResult struct {
	History []*wildBGStatsEntry
}
