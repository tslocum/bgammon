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

func recordGameResult(g *bgammon.Game, winType int) error {
	return nil
}

func serverStats(tz *time.Location) (*serverStatsResult, error) {
	return &serverStatsResult{}, nil
}

func wildBGStats(tz *time.Location) (*wildBGStatsResult, error) {
	return &wildBGStatsResult{}, nil
}
