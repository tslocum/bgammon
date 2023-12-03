//go:build database

package server

import (
	"context"
	"log"
	"time"

	"code.rocket9labs.com/tslocum/bgammon"
	"github.com/jackc/pgx/v5"
)

const databaseSchema = `
CREATE TABLE game (
	id      serial PRIMARY KEY,
	acey    integer NOT NULL,
	started bigint NOT NULL,
	ended   bigint NOT NULL,
	player1 text NOT NULL,
	player2 text NOT NULL,
	points  integer NOT NULL,
	winner  integer NOT NULL,
	wintype  integer NOT NULL
);
`

var db *pgx.Conn

func connectDB(dataSource string) error {
	var err error
	db, err = pgx.Connect(context.Background(), dataSource)
	return err
}

func begin() (pgx.Tx, error) {
	tx, err := db.Begin(context.Background())
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(context.Background(), "SET SCHEMA 'bgammon'")
	if err != nil {
		return nil, err
	}
	return tx, nil
}

func testDBConnection() error {
	_, err := db.Exec(context.Background(), "SELECT 1=1")
	return err
}

func initDB() {
	tx, err := begin()
	if err != nil {
		log.Fatalf("failed to initialize database: %s", err)
	}
	defer tx.Commit(context.Background())

	var result int
	err = tx.QueryRow(context.Background(), "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'bgammon' AND table_name = 'game'").Scan(&result)
	if err != nil {
		log.Fatal(err)
	} else if result > 0 {
		return // Database has been initialized.
	}

	_, err = tx.Exec(context.Background(), databaseSchema)
	if err != nil {
		log.Fatalf("failed to initialize database: %s", err)
	}
	log.Println("Initialized database schema")
}

func recordGameResult(g *bgammon.Game, winType int) error {
	if db == nil || g.Started.IsZero() || g.Ended.IsZero() || g.Winner == 0 {
		return nil
	}

	tx, err := begin()
	if err != nil {
		return err
	}
	defer tx.Commit(context.Background())

	acey := 0
	if g.Acey {
		acey = 1
	}
	_, err = tx.Exec(context.Background(), "INSERT INTO game (acey, started, ended, player1, player2, points, winner, wintype) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)", acey, g.Started.Unix(), g.Ended.Unix(), g.Player1.Name, g.Player2.Name, g.Points, g.Winner, winType)
	return err
}

func serverStats(tz *time.Location) (*serverStatsResult, error) {
	tx, err := begin()
	if err != nil {
		return nil, err
	}
	defer tx.Commit(context.Background())

	var earliestGame int64
	rows, err := tx.Query(context.Background(), "SELECT started FROM game ORDER BY started ASC LIMIT 1")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		if err != nil {
			continue
		}
		err = rows.Scan(&earliestGame)
	}
	if err != nil {
		return nil, err
	}

	result := &serverStatsResult{}
	earliest := midnight(time.Unix(earliestGame, 0).In(tz))
	rangeStart, rangeEnd := earliest.Unix(), earliest.AddDate(0, 0, 1).Unix()
	var count int
	for {
		rows, err := tx.Query(context.Background(), "SELECT COUNT(*) FROM game WHERE started >= $1 AND started < $2", rangeStart, rangeEnd)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			if err != nil {
				continue
			}
			err = rows.Scan(&count)
		}
		if err != nil {
			return nil, err
		}

		result.History = append(result.History, &serverStatsEntry{
			Date:  earliest.Format("2006-01-02"),
			Games: count,
		})

		earliest = earliest.AddDate(0, 0, 1)
		rangeStart, rangeEnd = rangeEnd, earliest.AddDate(0, 0, 1).Unix()
		if rangeStart >= time.Now().Unix() {
			break
		}
	}
	return result, nil
}

func botStats(name string, tz *time.Location) (*botStatsResult, error) {
	tx, err := begin()
	if err != nil {
		return nil, err
	}
	defer tx.Commit(context.Background())

	var earliestGame int64
	rows, err := tx.Query(context.Background(), "SELECT started FROM game WHERE player1 = $1 OR player2 = $2 ORDER BY started ASC LIMIT 1", name, name)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		if err != nil {
			continue
		}
		err = rows.Scan(&earliestGame)
	}
	if err != nil {
		return nil, err
	}

	result := &botStatsResult{}
	earliest := midnight(time.Unix(earliestGame, 0).In(tz))
	rangeStart, rangeEnd := earliest.Unix(), earliest.AddDate(0, 0, 1).Unix()
	var winCount, lossCount int
	for {
		rows, err := tx.Query(context.Background(), "SELECT COUNT(*) FROM game WHERE started >= $1 AND started < $2 AND (player1 = $3 OR player2 = $4)", rangeStart, rangeEnd, name, name)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			if err != nil {
				continue
			}
			err = rows.Scan(&lossCount)
		}
		if err != nil {
			return nil, err
		}

		rows, err = tx.Query(context.Background(), "SELECT COUNT(*) FROM game WHERE started >= $1 AND started < $2 AND ((player1 = $3 AND winner = 1) OR (player2 = $4 AND winner = 2))", rangeStart, rangeEnd, name, name)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			if err != nil {
				continue
			}
			err = rows.Scan(&winCount)
		}
		if err != nil {
			return nil, err
		}
		lossCount -= winCount

		if winCount != 0 || lossCount != 0 {
			result.History = append(result.History, &botStatsEntry{
				Date:    earliest.Format("2006-01-02"),
				Percent: (float64(winCount) / float64(winCount+lossCount)),
				Wins:    winCount,
				Losses:  lossCount,
			})
		}

		earliest = earliest.AddDate(0, 0, 1)
		rangeStart, rangeEnd = rangeEnd, earliest.AddDate(0, 0, 1).Unix()
		if rangeStart >= time.Now().Unix() {
			break
		}
	}
	return result, nil
}

func midnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}