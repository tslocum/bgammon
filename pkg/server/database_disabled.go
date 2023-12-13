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

func registerAccount(a *account) error {
	return nil
}

func loginAccount(username []byte, password []byte) (*account, error) {
	return nil, nil
}

func recordGameResult(g *bgammon.Game, winType int, account1 int, account2 int) error {
	return nil
}

func serverStats(tz *time.Location) (*serverStatsResult, error) {
	return &serverStatsResult{}, nil
}

func botStats(name string, tz *time.Location) (*botStatsResult, error) {
	return &botStatsResult{}, nil
}
