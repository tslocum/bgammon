package main

import (
	"context"
	"log"

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
