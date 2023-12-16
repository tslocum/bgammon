//go:build !database

package server

import (
	"time"

	"code.rocket9labs.com/tslocum/bgammon"
)

func connectDB(dataSource string) error {
	return nil
}

func testDBConnection() error {
	return nil
}

func initDB() {
}

func registerAccount(passwordSalt string, a *account) error {
	return nil
}

func resetAccount(mailServer string, resetSalt string, email []byte) error {
	return nil
}

func confirmResetAccount(resetSalt string, passwordSalt string, id int, key string) (string, error) {
	return "", nil
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

func recordGameResult(g *bgammon.Game, winType int, account1 int, account2 int) error {
	return nil
}

func dailyStats(tz *time.Location) (*serverStatsResult, error) {
	return &serverStatsResult{}, nil
}

func cumulativeStats(tz *time.Location) (*serverStatsResult, error) {
	return &serverStatsResult{}, nil
}

func botStats(name string, tz *time.Location) (*botStatsResult, error) {
	return &botStatsResult{}, nil
}
