package main

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
	started bigint NOT NULL,
	ended   bigint NOT NULL,
	winner  integer NOT NULL,
	player1 text NOT NULL,
	player2 text NOT NULL
);
`

func connectDB(dataSource string) (*pgx.Conn, error) {
	var err error
	db, err := pgx.Connect(context.Background(), dataSource)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func begin(db *pgx.Conn) (pgx.Tx, error) {
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

func testDBConnection(db *pgx.Conn) error {
	_, err := db.Exec(context.Background(), "SELECT 1=1")
	return err
}

func initDB(db *pgx.Conn) {
	tx, err := begin(db)
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

func recordGameResult(conn *pgx.Conn, g bgammon.Game) error {
	if g.Started.IsZero() || g.Ended.IsZero() || g.Winner == 0 {
		return nil
	}

	tx, err := begin(conn)
	if err != nil {
		return err
	}
	defer tx.Commit(context.Background())

	_, err = tx.Exec(context.Background(), "INSERT INTO game (started, ended, winner, player1, player2) VALUES ($1, $2, $3, $4, $5)", g.Started.Unix(), g.Ended.Unix(), g.Winner, g.Player1.Name, g.Player2.Name)
	return err
}

type serverStatsEntry struct {
	Date  string
	Games int
}

type serverStatsResult struct {
	History []*serverStatsEntry
}

func serverStats(conn *pgx.Conn, tz *time.Location) (*serverStatsResult, error) {
	tx, err := begin(conn)
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

func midnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
