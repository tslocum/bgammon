//go:build database

package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"math/rand"
	"mime/multipart"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"time"

	"code.rocket9labs.com/tslocum/bgammon"
	"github.com/alexedwards/argon2id"
	"github.com/jackc/pgx/v5"
	"github.com/jlouis/glicko2"
	"github.com/matcornic/hermes/v2"
)

const databaseSchema = `
CREATE TABLE account (
	id           serial PRIMARY KEY,
	created      bigint NOT NULL,
	confirmed    bigint NOT NULL DEFAULT 0,
	active       bigint NOT NULL,
	reset        bigint NOT NULL DEFAULT 0,
	email        text NOT NULL,
	username     text NOT NULL,
	password     text NOT NULL,
	casualsingle integer NOT NULL DEFAULT 150000,
	casualmulti  integer NOT NULL DEFAULT 150000,
	ratedsingle  integer NOT NULL DEFAULT 150000,
	ratedmulti   integer NOT NULL DEFAULT 150000,
	highlight    smallint NOT NULL DEFAULT 1,
	pips         smallint NOT NULL DEFAULT 1,
	moves        smallint NOT NULL DEFAULT 0
);
CREATE TABLE game (
	id       serial PRIMARY KEY,
	acey     integer NOT NULL,
	started  bigint NOT NULL,
	ended    bigint NOT NULL,
	player1  text NOT NULL,
	account1 integer NOT NULL,
	player2  text NOT NULL,
	account2 integer NOT NULL,
	points   integer NOT NULL,
	winner   integer NOT NULL,
	wintype  integer NOT NULL,
	replay   TEXT NOT NULL DEFAULT ''
);
`

var (
	db     *pgx.Conn
	dbLock = &sync.Mutex{}
)

var passwordArgon2id = &argon2id.Params{
	Memory:      128 * 1024,
	Iterations:  16,
	Parallelism: 4,
	SaltLength:  16,
	KeyLength:   64,
}

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

func registerAccount(passwordSalt string, a *account) error {
	dbLock.Lock()
	defer dbLock.Unlock()

	if db == nil {
		return nil
	} else if len(bytes.TrimSpace(a.username)) == 0 {
		return fmt.Errorf("please enter a username")
	} else if len(bytes.TrimSpace(a.email)) == 0 {
		return fmt.Errorf("please enter an email address")
	} else if len(bytes.TrimSpace(a.password)) == 0 {
		return fmt.Errorf("please enter a password")
	} else if !bytes.ContainsRune(a.email, '@') || !bytes.ContainsRune(a.email, '.') {
		return fmt.Errorf("please enter a valid email address")
	} else if !alphaNumericUnderscore.Match(a.username) {
		return fmt.Errorf("please enter a username containing only letters, numbers and underscores")
	} else if bytes.HasPrefix(bytes.ToLower(a.username), []byte("guest_")) {
		return fmt.Errorf("please enter a valid username")
	}

	tx, err := begin()
	if err != nil {
		return err
	}
	defer tx.Commit(context.Background())

	var result int
	err = tx.QueryRow(context.Background(), "SELECT COUNT(*) FROM account WHERE email = $1", bytes.ToLower(bytes.TrimSpace(a.email))).Scan(&result)
	if err != nil {
		log.Fatal(err)
	} else if result > 0 {
		return fmt.Errorf("email address already in use")
	}

	err = tx.QueryRow(context.Background(), "SELECT COUNT(*) FROM account WHERE username = $1", bytes.ToLower(bytes.TrimSpace(a.username))).Scan(&result)
	if err != nil {
		log.Fatal(err)
	} else if result > 0 {
		return fmt.Errorf("username already in use")
	}

	passwordHash, err := argon2id.CreateHash(string(a.password)+passwordSalt, passwordArgon2id)
	if err != nil {
		return err
	}

	timestamp := time.Now().Unix()
	_, err = tx.Exec(context.Background(), "INSERT INTO account (created, active, email, username, password) VALUES ($1, $2, $3, $4, $5)", timestamp, timestamp, bytes.ToLower(bytes.TrimSpace(a.email)), bytes.ToLower(bytes.TrimSpace(a.username)), passwordHash)
	return err
}

func resetAccount(mailServer string, resetSalt string, email []byte) error {
	dbLock.Lock()
	defer dbLock.Unlock()

	if db == nil {
		return nil
	} else if len(bytes.TrimSpace(email)) == 0 {
		return fmt.Errorf("please enter an email address")
	}

	tx, err := begin()
	if err != nil {
		return err
	}
	defer tx.Commit(context.Background())

	var result int
	err = tx.QueryRow(context.Background(), "SELECT COUNT(*) FROM account WHERE email = $1", bytes.ToLower(bytes.TrimSpace(email))).Scan(&result)
	if err != nil {
		return err
	} else if result == 0 {
		return nil
	}

	var (
		id           int
		reset        int64
		accountEmail []byte
		passwordHash []byte
	)
	err = tx.QueryRow(context.Background(), "SELECT id, reset, email, password FROM account WHERE email = $1", bytes.ToLower(bytes.TrimSpace(email))).Scan(&id, &reset, &accountEmail, &passwordHash)
	if err != nil {
		return err
	} else if id == 0 || len(passwordHash) == 0 {
		return nil
	}

	const resetTimeout = 86400 // 24 hours.
	if time.Now().Unix()-reset >= resetTimeout {
		timestamp := time.Now().Unix()

		h := sha256.New()
		h.Write([]byte(fmt.Sprintf("%d/%d", id, timestamp) + resetSalt))
		hash := fmt.Sprintf("%x", h.Sum(nil))[0:16]

		emailConfig := hermes.Hermes{
			Product: hermes.Product{
				Name:      "https://bgammon.org",
				Link:      " ",
				Copyright: " ",
			},
		}

		resetEmail := hermes.Email{
			Body: hermes.Body{
				Greeting: "Hello",
				Intros: []string{
					"You are receiving this email because you (or someone else) requested to reset your bgammon.org password.",
				},
				Actions: []hermes.Action{
					{
						Instructions: "Click to reset your password:",
						Button: hermes.Button{
							Color: "#DC4D2F",
							Text:  "Reset your password",
							Link:  "https://bgammon.org/reset/" + strconv.Itoa(id) + "/" + hash,
						},
					},
				},
				Outros: []string{
					"If you did not request to reset your bgammon.org password, no further action is required on your part.",
				},
				Signature: "Ciao",
			},
		}
		emailPlain, err := emailConfig.GeneratePlainText(resetEmail)
		if err != nil {
			return nil
		}
		emailPlain = strings.ReplaceAll(emailPlain, "https://bgammon.org -", "https://bgammon.org")

		emailHTML, err := emailConfig.GenerateHTML(resetEmail)
		if err != nil {
			return nil
		}

		if sendEmail(mailServer, string(accountEmail), "Reset your bgammon.org password", emailPlain, emailHTML) {
			_, err = tx.Exec(context.Background(), "UPDATE account SET reset = $1 WHERE id = $2", timestamp, id)
		}
		return err
	}
	return nil
}

func confirmResetAccount(resetSalt string, passwordSalt string, id int, key string) (string, error) {
	dbLock.Lock()
	defer dbLock.Unlock()

	if db == nil {
		return "", nil
	} else if id == 0 {
		return "", fmt.Errorf("no id provided")
	} else if len(strings.TrimSpace(key)) == 0 {
		return "", fmt.Errorf("no reset key provided")
	}

	tx, err := begin()
	if err != nil {
		return "", err
	}
	defer tx.Commit(context.Background())

	var result int
	err = tx.QueryRow(context.Background(), "SELECT COUNT(*) FROM account WHERE id = $1 AND reset != 0", id).Scan(&result)
	if err != nil {
		return "", err
	} else if result == 0 {
		return "", nil
	}

	var reset int
	err = tx.QueryRow(context.Background(), "SELECT reset FROM account WHERE id = $1", id).Scan(&reset)
	if err != nil {
		return "", err
	}

	h := sha256.New()
	h.Write([]byte(fmt.Sprintf("%d/%d", id, reset) + resetSalt))
	hash := fmt.Sprintf("%x", h.Sum(nil))[0:16]
	if key != hash {
		return "", nil
	}

	newPassword := randomAlphanumeric(7)

	passwordHash, err := argon2id.CreateHash(newPassword+passwordSalt, passwordArgon2id)
	if err != nil {
		return "", err
	}

	_, err = tx.Exec(context.Background(), "UPDATE account SET password = $1, reset = reset - 1 WHERE id = $2", passwordHash, id)
	return newPassword, err
}

func loginAccount(passwordSalt string, username []byte, password []byte) (*account, error) {
	dbLock.Lock()
	defer dbLock.Unlock()

	if db == nil {
		return nil, nil
	} else if len(bytes.TrimSpace(username)) == 0 {
		return nil, fmt.Errorf("please enter a username")
	} else if len(bytes.TrimSpace(password)) == 0 {
		return nil, fmt.Errorf("please enter a password")
	}

	tx, err := begin()
	if err != nil {
		return nil, err
	}
	defer tx.Commit(context.Background())

	a := &account{}
	var highlight, pips, moves int
	err = tx.QueryRow(context.Background(), "SELECT id, email, username, password, highlight, pips, moves FROM account WHERE username = $1 OR email = $2", bytes.ToLower(bytes.TrimSpace(username)), bytes.ToLower(bytes.TrimSpace(username))).Scan(&a.id, &a.email, &a.username, &a.password, &highlight, &pips, &moves)
	if err != nil {
		return nil, nil
	} else if len(a.password) == 0 {
		return nil, fmt.Errorf("account disabled")
	}
	a.highlight = highlight == 1
	a.pips = pips == 1
	a.moves = moves == 1

	match, err := argon2id.ComparePasswordAndHash(string(password)+passwordSalt, string(a.password))
	if err != nil {
		return nil, err
	} else if !match {
		return nil, nil
	}
	return a, nil
}

func setAccountPassword(passwordSalt string, id int, password string) error {
	dbLock.Lock()
	defer dbLock.Unlock()

	if db == nil {
		return nil
	} else if id <= 0 {
		return fmt.Errorf("no id provided")
	} else if len(strings.TrimSpace(password)) == 0 {
		return fmt.Errorf("no password provided")
	}

	tx, err := begin()
	if err != nil {
		return err
	}
	defer tx.Commit(context.Background())

	var result int
	err = tx.QueryRow(context.Background(), "SELECT COUNT(*) FROM account WHERE id = $1", id).Scan(&result)
	if err != nil {
		return err
	} else if result == 0 {
		return nil
	}

	passwordHash, err := argon2id.CreateHash(password+passwordSalt, passwordArgon2id)
	if err != nil {
		return err
	}

	_, err = tx.Exec(context.Background(), "UPDATE account SET password = $1 WHERE id = $2", passwordHash, id)
	return err
}

func setAccountSetting(id int, name string, value int) error {
	dbLock.Lock()
	defer dbLock.Unlock()

	if db == nil {
		return nil
	} else if name == "" {
		return fmt.Errorf("no setting name provided")
	}

	tx, err := begin()
	if err != nil {
		return err
	}
	defer tx.Commit(context.Background())

	var result int
	err = tx.QueryRow(context.Background(), "SELECT COUNT(*) FROM account WHERE id = $1", id).Scan(&result)
	if err != nil {
		return err
	} else if result == 0 {
		return nil
	}

	_, err = tx.Exec(context.Background(), "UPDATE account SET "+name+" = $1 WHERE id = $2", value, id)
	return err
}

func recordGameResult(g *bgammon.Game, winType int, account1 int, account2 int, replay [][]byte) error {
	dbLock.Lock()
	defer dbLock.Unlock()

	if db == nil || g.Started.IsZero() || g.Winner == 0 {
		return nil
	}

	ended := g.Ended
	if ended.IsZero() {
		ended = time.Now()
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
	_, err = tx.Exec(context.Background(), "INSERT INTO game (acey, started, ended, player1, account1, player2, account2, points, winner, wintype, replay) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)", acey, g.Started.Unix(), ended.Unix(), g.Player1.Name, account1, g.Player2.Name, account2, g.Points, g.Winner, winType, bytes.Join(replay, []byte("\n")))
	return err
}

func recordMatchResult(g *bgammon.Game, matchType int, account1 int, account2 int) error {
	dbLock.Lock()
	defer dbLock.Unlock()

	if db == nil || g.Started.IsZero() || g.Winner == 0 || account1 == 0 || account2 == 0 || account1 == account2 {
		return nil
	}

	tx, err := begin()
	if err != nil {
		return err
	}
	defer tx.Commit(context.Background())

	var columnName string
	switch matchType {
	case matchTypeCasual:
		if g.Points == 1 {
			columnName = "casualsingle"
		} else {
			columnName = "casualmulti"
		}
	case matchTypeRated:
		if g.Points == 1 {
			columnName = "ratedsingle"
		} else {
			columnName = "ratedmulti"
		}
	default:
		log.Panicf("unknown match type: %d", matchType)
	}

	var rating1i int
	err = tx.QueryRow(context.Background(), "SELECT "+columnName+" FROM account WHERE id = $1", account1).Scan(&rating1i)
	if err != nil {
		return err
	}
	rating1 := float64(rating1i) / 100

	var rating2i int
	err = tx.QueryRow(context.Background(), "SELECT "+columnName+" FROM account WHERE id = $1", account2).Scan(&rating2i)
	if err != nil {
		return err
	}
	rating2 := float64(rating2i) / 100

	outcome1, outcome2 := 1.0, 0.0
	if g.Winner == 2 {
		outcome1, outcome2 = 0.0, 1.0
	}
	rating1New, _, _ := glicko2.Rank(rating1, 50, 0.06, []glicko2.Opponent{ratingPlayer{rating2, 30, 0.06, outcome1}}, 0.6)
	rating2New, _, _ := glicko2.Rank(rating2, 50, 0.06, []glicko2.Opponent{ratingPlayer{rating1, 30, 0.06, outcome2}}, 0.6)

	_, err = tx.Exec(context.Background(), "UPDATE account SET "+columnName+" = $1 WHERE id = $2", int(rating1New*100), account1)
	if err != nil {
		return err
	}

	_, err = tx.Exec(context.Background(), "UPDATE account SET "+columnName+" = $1 WHERE id = $2", int(rating2New*100), account2)
	return err
}

func matchInfo(id int) (timestamp int64, player1 string, player2 string, replay []byte, err error) {
	dbLock.Lock()
	defer dbLock.Unlock()

	if db == nil {
		return 0, "", "", nil, err
	} else if id <= 0 {
		return 0, "", "", nil, fmt.Errorf("please specify an id")
	}

	tx, err := begin()
	if err != nil {
		return 0, "", "", nil, err
	}
	defer tx.Commit(context.Background())

	err = tx.QueryRow(context.Background(), "SELECT started, player1, player2, replay FROM game WHERE id = $1 AND replay != ''", id).Scan(&timestamp, &player1, &player2, &replay)
	if err != nil {
		return 0, "", "", nil, err
	}
	return timestamp, player1, player2, replay, nil
}

func replayByID(id int) ([]byte, error) {
	dbLock.Lock()
	defer dbLock.Unlock()

	if db == nil {
		return nil, nil
	} else if id <= 0 {
		return nil, fmt.Errorf("please specify an id")
	}

	tx, err := begin()
	if err != nil {
		return nil, err
	}
	defer tx.Commit(context.Background())

	var replay []byte
	err = tx.QueryRow(context.Background(), "SELECT replay FROM game WHERE id = $1", id).Scan(&replay)
	if err != nil {
		return nil, nil
	}
	return replay, nil
}

func matchHistory(username string) ([]*bgammon.HistoryMatch, error) {
	dbLock.Lock()
	defer dbLock.Unlock()

	tx, err := begin()
	if err != nil {
		return nil, err
	}
	defer tx.Commit(context.Background())

	username = strings.ToLower(username)

	var matches []*bgammon.HistoryMatch
	var player1, player2 string
	var winner int
	rows, err := tx.Query(context.Background(), "SELECT id, started, player1, player2, points, winner FROM game WHERE (LOWER(player1) = $1 OR LOWER(player2) = $2) AND replay != '' ORDER BY id DESC", username, username)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		if err != nil {
			continue
		}
		match := &bgammon.HistoryMatch{}
		err = rows.Scan(&match.ID, &match.Timestamp, &player1, &player2, &match.Points, &winner)
		if err != nil {
			continue
		}
		if strings.ToLower(player1) == username {
			match.Winner, match.Opponent = winner, player2
		} else {
			match.Winner, match.Opponent = 1+(2-winner), player1
		}
		matches = append(matches, match)
	}
	if err != nil {
		return nil, err
	}
	return matches, nil
}

func getLeaderboard(matchType int, multiPoint bool) (*leaderboardResult, error) {
	dbLock.Lock()
	defer dbLock.Unlock()

	tx, err := begin()
	if err != nil {
		return nil, err
	}
	defer tx.Commit(context.Background())

	var columnName string
	switch matchType {
	case matchTypeCasual:
		if !multiPoint {
			columnName = "casualsingle"
		} else {
			columnName = "casualmulti"
		}
	case matchTypeRated:
		if !multiPoint {
			columnName = "ratedsingle"
		} else {
			columnName = "ratedmulti"
		}
	default:
		log.Panicf("unknown match type: %d", matchType)
	}

	result := &leaderboardResult{}
	rows, err := tx.Query(context.Background(), "SELECT username, "+columnName+" FROM account ORDER BY "+columnName+" DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		if err != nil {
			continue
		}
		entry := &leaderboardEntry{}
		err = rows.Scan(&entry.User, &entry.Rating)
		if err != nil {
			continue
		}
		entry.Rating /= 100
		result.Leaderboard = append(result.Leaderboard, entry)
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

func dailyStats(tz *time.Location) (*serverStatsResult, error) {
	dbLock.Lock()
	defer dbLock.Unlock()

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

func cumulativeStats(tz *time.Location) (*serverStatsResult, error) {
	dbLock.Lock()
	defer dbLock.Unlock()

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
	var total int
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

		total += count

		result.History = append(result.History, &serverStatsEntry{
			Date:  earliest.Format("2006-01-02"),
			Games: total,
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
	dbLock.Lock()
	defer dbLock.Unlock()

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

func sendEmail(mailServer string, emailAddress string, emailSubject string, emailPlain string, emailHTML string) bool {
	mixedContent := &bytes.Buffer{}
	mixedWriter := multipart.NewWriter(mixedContent)
	var newBoundary = "RELATED-" + mixedWriter.Boundary()
	mixedWriter.SetBoundary(first70("MIXED-" + mixedWriter.Boundary()))
	relatedWriter, newBoundary := nestedMultipart(mixedWriter, "multipart/related", newBoundary)
	altWriter, newBoundary := nestedMultipart(relatedWriter, "multipart/alternative", "ALTERNATIVE-"+newBoundary)

	var childContent io.Writer
	childContent, _ = altWriter.CreatePart(textproto.MIMEHeader{"Content-Type": {"text/plain"}})
	childContent.Write([]byte(emailPlain))
	childContent, _ = altWriter.CreatePart(textproto.MIMEHeader{"Content-Type": {"text/html"}})
	childContent.Write([]byte(emailHTML))

	altWriter.Close()
	relatedWriter.Close()
	mixedWriter.Close()

	if mailServer == "" {
		fmt.Print(`From: bgammon.org <noreply@bgammon.org>
	To: <` + emailAddress + `>
	Subject: ` + emailSubject + `
	MIME-Version: 1.0
	Content-Type: multipart/mixed; boundary=`)
		fmt.Print(mixedWriter.Boundary(), "\n\n")
		fmt.Println(mixedContent.String())
		return true
	}

	c, err := smtp.Dial(mailServer)
	if err != nil {
		return false
	}
	defer c.Close()

	c.Mail("noreply@bgammon.org")
	c.Rcpt(emailAddress)

	wc, err := c.Data()
	if err != nil {
		return false
	}
	defer wc.Close()

	fmt.Fprint(wc, `From: bgammon.org <noreply@bgammon.org>
To: `+emailAddress+`
Subject: `+emailSubject+`
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary=`)
	fmt.Fprint(wc, mixedWriter.Boundary(), "\n\n")
	fmt.Fprintln(wc, mixedContent.String())
	return true
}

func nestedMultipart(enclosingWriter *multipart.Writer, contentType, boundary string) (nestedWriter *multipart.Writer, newBoundary string) {

	var contentBuffer io.Writer
	var err error

	boundary = first70(boundary)
	contentWithBoundary := contentType + "; boundary=\"" + boundary + "\""
	contentBuffer, err = enclosingWriter.CreatePart(textproto.MIMEHeader{"Content-Type": {contentWithBoundary}})
	if err != nil {
		log.Fatal(err)
	}

	nestedWriter = multipart.NewWriter(contentBuffer)
	newBoundary = nestedWriter.Boundary()
	nestedWriter.SetBoundary(boundary)
	return
}

func first70(str string) string {
	if len(str) > 70 {
		return string(str[0:69])
	}
	return str
}

var letters = []rune("abcdefghkmnpqrstwxyzABCDEFGHJKMNPQRSTWXYZ23456789")

func randomAlphanumeric(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
