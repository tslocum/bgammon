//go:build !full

package server

import (
	"time"

	"codeberg.org/tslocum/bgammon"
)

func connectDB(dataSource string) error {
	return nil
}

func testDBConnection() error {
	return nil
}

func initDB() {
}

func registerAccount(passwordSalt string, a *account, ipHash string) error {
	return nil
}

func resetAccount(mailServer string, resetSalt string, email []byte) error {
	return nil
}

func confirmResetAccount(resetSalt string, passwordSalt string, id int, key string) (string, string, error) {
	return "", "", nil
}

func accountByID(id int) (*account, error) {
	return nil, nil
}

func accountByUsername(username string) (*account, error) {
	return nil, nil
}

func loginAccount(passwordSalt string, username []byte, password []byte) (*account, error) {
	return nil, nil
}

func setAccountPassword(passwordSalt string, id int, password string) error {
	return nil
}

func setAccountSetting(id int, name string, value int) error {
	return nil
}

func setAccountFollows(id int, target int, follows bool) error {
	return nil
}

func awardAchievement(a *account, award int, game int, date int64) (bool, error) {
	return false, nil
}

func matchInfo(id int) (timestamp int64, player1 string, player2 string, replay []byte, err error) {
	return 0, "", "", nil, nil
}

func replayByID(id int) ([]byte, error) {
	return nil, nil
}

func addBan(ipHash string, account int, staff int, reason string) error {
	return nil
}

func checkBan(ipHash string, account int) (bool, string) {
	return false, ""
}

func deleteBan(ipHash string, account int) error {
	return nil
}

func recordGameResult(g *serverGame, winType int8, replay [][]byte) (int, error) {
	return 0, nil
}

func recordMatchResult(g *serverGame, matchType int) (int, error) {
	return 0, nil
}

func matchHistory(username string) ([]*bgammon.HistoryMatch, error) {
	return nil, nil
}

func getLeaderboard(matchType int, variant int8, multiPoint bool) (*leaderboardResult, error) {
	return nil, nil
}

func dailyStats(tz *time.Location) (*serverStatsResult, error) {
	return &serverStatsResult{}, nil
}

func monthlyStats(tz *time.Location) (*serverStatsResult, error) {
	return &serverStatsResult{}, nil
}

func cumulativeStats(tz *time.Location) (*serverStatsResult, error) {
	return &serverStatsResult{}, nil
}

func accountStats(name string, tz *time.Location) (*accountStatsResult, error) {
	return &accountStatsResult{}, nil
}

func achievementStats() (*achievementStatsResult, error) {
	return &achievementStatsResult{}, nil
}

func playerVsPlayerStats(tz *time.Location) (*serverStatsResult, error) {
	return &serverStatsResult{}, nil
}
