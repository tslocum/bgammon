//go:build full

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
	"runtime/debug"
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
	id                       serial PRIMARY KEY,
	created                  bigint NOT NULL,
	createdip                text NOT NULL,
	confirmed                bigint NOT NULL DEFAULT 0,
	active                   bigint NOT NULL,
	reset                    bigint NOT NULL DEFAULT 0,
	email                    text NOT NULL,
	username                 text NOT NULL,
	password                 text NOT NULL,
	icon                     integer NOT NULL DEFAULT 0,
	icons                    text NOT NULL DEFAULT '',
	casual_backgammon_single integer NOT NULL DEFAULT 150000,
	casual_backgammon_multi  integer NOT NULL DEFAULT 150000,
	casual_acey_single       integer NOT NULL DEFAULT 150000,
	casual_acey_multi        integer NOT NULL DEFAULT 150000,
	casual_tabula_single     integer NOT NULL DEFAULT 150000,
	casual_tabula_multi      integer NOT NULL DEFAULT 150000,
	rated_backgammon_single  integer NOT NULL DEFAULT 150000,
	rated_backgammon_multi   integer NOT NULL DEFAULT 150000,
	rated_acey_single        integer NOT NULL DEFAULT 150000,
	rated_acey_multi         integer NOT NULL DEFAULT 150000,
	rated_tabula_single      integer NOT NULL DEFAULT 150000,
	rated_tabula_multi       integer NOT NULL DEFAULT 150000,
	autoplay                 smallint NOT NULL DEFAULT 0,
	highlight                smallint NOT NULL DEFAULT 1,
	pips                     smallint NOT NULL DEFAULT 1,
	moves                    smallint NOT NULL DEFAULT 0,
	flip                     smallint NOT NULL DEFAULT 0,
	traditional              smallint NOT NULL DEFAULT 0,
	advanced                 smallint NOT NULL DEFAULT 0,
	mutejoinleave            smallint NOT NULL DEFAULT 0,
	mutechat                 smallint NOT NULL DEFAULT 0,
	muteroll                 smallint NOT NULL DEFAULT 0,
	mutemove                 smallint NOT NULL DEFAULT 0,
	mutebearoff              smallint NOT NULL DEFAULT 0,
	speed                    smallint NOT NULL DEFAULT 1
);
CREATE TABLE game (
	id       serial PRIMARY KEY,
	variant  smallint NOT NULL,
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
CREATE INDEX ON game USING btree (started);
CREATE TABLE follow (
	account integer NOT NULL,
	target integer NOT NULL,
	UNIQUE (account, target),
	CONSTRAINT follow_user
		FOREIGN KEY(account) 
		REFERENCES account(id),
	CONSTRAINT follow_target
		FOREIGN KEY(target) 
		REFERENCES account(id)
);
CREATE TABLE ban (
	ip text NOT NULL,
	account integer NOT NULL,
	created integer NOT NULL,
	staff integer NOT NULL,
	reason text NOT NULL,
	UNIQUE (ip, account)
);
`

var (
	db     *pgx.Conn
	dbLock = &sync.Mutex{}
)

var argon2idParameters = &argon2id.Params{
	Memory:      128 * 1024,
	Iterations:  2,
	Parallelism: 2,
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

func registerAccount(passwordSalt string, a *account, ipHash string) error {
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
	err = tx.QueryRow(context.Background(), "SELECT COUNT(*) FROM account WHERE createdip = $1", ipHash).Scan(&result)
	if err != nil {
		log.Fatal(err)
	} else if result > 0 {
		return fmt.Errorf("an account has already been registered from your IP address")
	}

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

	passwordHash, err := argon2id.CreateHash(string(a.password)+passwordSalt, argon2idParameters)
	debug.FreeOSMemory() // Hashing is memory intensive. Return memory to the OS.
	if err != nil {
		return err
	}

	timestamp := time.Now().Unix()
	_, err = tx.Exec(context.Background(), "INSERT INTO account (created, createdip, active, email, username, password) VALUES ($1, $2, $3, $4, $5, $6)", timestamp, ipHash, timestamp, bytes.ToLower(bytes.TrimSpace(a.email)), bytes.ToLower(bytes.TrimSpace(a.username)), passwordHash)
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

func confirmResetAccount(resetSalt string, passwordSalt string, id int, key string) (string, string, error) {
	dbLock.Lock()
	defer dbLock.Unlock()

	if db == nil {
		return "", "", nil
	} else if id == 0 {
		return "", "", fmt.Errorf("no id provided")
	} else if len(strings.TrimSpace(key)) == 0 {
		return "", "", fmt.Errorf("no reset key provided")
	}

	tx, err := begin()
	if err != nil {
		return "", "", err
	}
	defer tx.Commit(context.Background())

	var result int
	err = tx.QueryRow(context.Background(), "SELECT COUNT(*) FROM account WHERE id = $1 AND reset != 0", id).Scan(&result)
	if err != nil {
		return "", "", err
	} else if result == 0 {
		return "", "", nil
	}

	var username string
	var reset int
	err = tx.QueryRow(context.Background(), "SELECT username, reset FROM account WHERE id = $1", id).Scan(&username, &reset)
	if err != nil {
		return "", "", err
	}

	h := sha256.New()
	h.Write([]byte(fmt.Sprintf("%d/%d", id, reset) + resetSalt))
	hash := fmt.Sprintf("%x", h.Sum(nil))[0:16]
	if key != hash {
		return "", "", nil
	}

	newPassword := randomAlphanumeric(7)

	passwordHash, err := argon2id.CreateHash(newPassword+passwordSalt, argon2idParameters)
	debug.FreeOSMemory() // Hashing is memory intensive. Return memory to the OS.
	if err != nil {
		return "", "", err
	}

	_, err = tx.Exec(context.Background(), "UPDATE account SET password = $1, reset = reset - 1 WHERE id = $2", passwordHash, id)
	return username, newPassword, err
}

func accountByID(id int) (*account, error) {
	dbLock.Lock()
	defer dbLock.Unlock()

	if db == nil || id <= 0 {
		return nil, nil
	}

	tx, err := begin()
	if err != nil {
		return nil, err
	}
	defer tx.Commit(context.Background())

	a := &account{
		casual:      &clientRating{},
		competitive: &clientRating{},
	}
	var autoplay, highlight, pips, moves, flip, traditional, advanced, muteJoinLeave, muteChat, muteRoll, muteMove, muteBearOff int
	err = tx.QueryRow(context.Background(), "SELECT id, email, username, password, icon, autoplay, highlight, pips, moves, flip, traditional, advanced, mutejoinleave, mutechat, muteroll, mutemove, mutebearoff, speed, casual_backgammon_single, casual_backgammon_multi, casual_acey_single, casual_acey_multi, casual_tabula_single, casual_tabula_multi, rated_backgammon_single, rated_backgammon_multi, rated_acey_single, rated_acey_multi, rated_tabula_single, rated_tabula_multi FROM account WHERE id = $1", id).Scan(&a.id, &a.email, &a.username, &a.password, &a.icon, &autoplay, &highlight, &pips, &moves, &flip, &traditional, &advanced, &muteJoinLeave, &muteChat, &muteRoll, &muteMove, &muteBearOff, &a.speed, &a.casual.backgammonSingle, &a.casual.backgammonMulti, &a.casual.aceySingle, &a.casual.aceyMulti, &a.casual.tabulaSingle, &a.casual.tabulaMulti, &a.competitive.backgammonSingle, &a.competitive.backgammonMulti, &a.competitive.aceySingle, &a.competitive.aceyMulti, &a.competitive.tabulaSingle, &a.competitive.tabulaMulti)
	if err != nil {
		return nil, nil
	}
	a.autoplay = autoplay == 1
	a.highlight = highlight == 1
	a.pips = pips == 1
	a.moves = moves == 1
	a.flip = flip == 1
	a.traditional = traditional == 1
	a.advanced = advanced == 1
	a.muteJoinLeave = muteJoinLeave == 1
	a.muteChat = muteChat == 1
	a.muteRoll = muteRoll == 1
	a.muteMove = muteMove == 1
	a.muteBearOff = muteBearOff == 1
	return a, nil
}

func accountByUsername(username string) (*account, error) {
	dbLock.Lock()
	defer dbLock.Unlock()

	if db == nil || len(strings.TrimSpace(username)) == 0 {
		return nil, nil
	}

	tx, err := begin()
	if err != nil {
		return nil, err
	}
	defer tx.Commit(context.Background())

	a := &account{
		casual:      &clientRating{},
		competitive: &clientRating{},
	}
	var autoplay, highlight, pips, moves, flip, advanced, muteJoinLeave, muteChat, muteRoll, muteMove, muteBearOff int
	err = tx.QueryRow(context.Background(), "SELECT id, email, username, password, icon, autoplay, highlight, pips, moves, flip, advanced, mutejoinleave, mutechat, muteroll, mutemove, mutebearoff, speed, casual_backgammon_single, casual_backgammon_multi, casual_acey_single, casual_acey_multi, casual_tabula_single, casual_tabula_multi, rated_backgammon_single, rated_backgammon_multi, rated_acey_single, rated_acey_multi, rated_tabula_single, rated_tabula_multi FROM account WHERE username = $1", strings.ToLower(username)).Scan(&a.id, &a.email, &a.username, &a.password, &a.icon, &autoplay, &highlight, &pips, &moves, &flip, &advanced, &muteJoinLeave, &muteChat, &muteRoll, &muteMove, &muteBearOff, &a.speed, &a.casual.backgammonSingle, &a.casual.backgammonMulti, &a.casual.aceySingle, &a.casual.aceyMulti, &a.casual.tabulaSingle, &a.casual.tabulaMulti, &a.competitive.backgammonSingle, &a.competitive.backgammonMulti, &a.competitive.aceySingle, &a.competitive.aceyMulti, &a.competitive.tabulaSingle, &a.competitive.tabulaMulti)
	if err != nil {
		return nil, nil
	}
	a.autoplay = autoplay == 1
	a.highlight = highlight == 1
	a.pips = pips == 1
	a.moves = moves == 1
	a.flip = flip == 1
	a.advanced = advanced == 1
	a.muteJoinLeave = muteJoinLeave == 1
	a.muteChat = muteChat == 1
	a.muteRoll = muteRoll == 1
	a.muteMove = muteMove == 1
	a.muteBearOff = muteBearOff == 1
	return a, nil
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

	a := &account{
		casual:      &clientRating{},
		competitive: &clientRating{},
	}
	var autoplay, highlight, pips, moves, flip, advanced, muteJoinLeave, muteChat, muteRoll, muteMove, muteBearOff int
	err = tx.QueryRow(context.Background(), "SELECT id, email, username, password, icon, autoplay, highlight, pips, moves, flip, advanced, mutejoinleave, mutechat, muteroll, mutemove, mutebearoff, speed, casual_backgammon_single, casual_backgammon_multi, casual_acey_single, casual_acey_multi, casual_tabula_single, casual_tabula_multi, rated_backgammon_single, rated_backgammon_multi, rated_acey_single, rated_acey_multi, rated_tabula_single, rated_tabula_multi FROM account WHERE username = $1 OR email = $2", bytes.ToLower(bytes.TrimSpace(username)), bytes.ToLower(bytes.TrimSpace(username))).Scan(&a.id, &a.email, &a.username, &a.password, &a.icon, &autoplay, &highlight, &pips, &moves, &flip, &advanced, &muteJoinLeave, &muteChat, &muteRoll, &muteMove, &muteBearOff, &a.speed, &a.casual.backgammonSingle, &a.casual.backgammonMulti, &a.casual.aceySingle, &a.casual.aceyMulti, &a.casual.tabulaSingle, &a.casual.tabulaMulti, &a.competitive.backgammonSingle, &a.competitive.backgammonMulti, &a.competitive.aceySingle, &a.competitive.aceyMulti, &a.competitive.tabulaSingle, &a.competitive.tabulaMulti)
	if err != nil {
		return nil, nil
	} else if len(a.password) == 0 {
		return nil, fmt.Errorf("account disabled")
	}
	a.autoplay = autoplay == 1
	a.highlight = highlight == 1
	a.pips = pips == 1
	a.moves = moves == 1
	a.flip = flip == 1
	a.advanced = advanced == 1
	a.muteJoinLeave = muteJoinLeave == 1
	a.muteChat = muteChat == 1
	a.muteRoll = muteRoll == 1
	a.muteMove = muteMove == 1
	a.muteBearOff = muteBearOff == 1

	match, err := argon2id.ComparePasswordAndHash(string(password)+passwordSalt, string(a.password))
	debug.FreeOSMemory() // Hashing is memory intensive. Return memory to the OS.
	if err != nil {
		return nil, err
	} else if !match {
		return nil, nil
	}

	var follows []byte
	err = tx.QueryRow(context.Background(), "select string_agg(target::text, ',') FROM follow WHERE account = $1", a.id).Scan(&follows)
	if err != nil {
		return nil, nil
	}
	for _, target := range bytes.Split(follows, []byte(",")) {
		v, err := strconv.Atoi(string(target))
		if err != nil || v <= 0 {
			continue
		}
		a.follows = append(a.follows, v)
	}

	_, err = tx.Exec(context.Background(), "UPDATE account SET active = $1 WHERE id = $2", time.Now().Unix(), a.id)
	if err != nil {
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

	passwordHash, err := argon2id.CreateHash(password+passwordSalt, argon2idParameters)
	debug.FreeOSMemory() // Hashing is memory intensive. Return memory to the OS.
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

func setAccountFollows(id int, target int, follows bool) error {
	dbLock.Lock()
	defer dbLock.Unlock()

	if db == nil {
		return nil
	} else if id == 0 || target == 0 {
		return fmt.Errorf("invalid id or target: %d/%d", id, target)
	}

	tx, err := begin()
	if err != nil {
		return err
	}
	defer tx.Commit(context.Background())

	if !follows {
		_, err = tx.Exec(context.Background(), "DELETE FROM follow WHERE account = $1 AND target = $2", id, target)
		return err
	}
	_, err = tx.Exec(context.Background(), "INSERT INTO follow VALUES ($1, $2)", id, target)
	return err
}

func recordGameResult(g *serverGame, winType int8, replay [][]byte) error {
	dbLock.Lock()
	defer dbLock.Unlock()

	if db == nil || g.Started == 0 || g.Winner == 0 || len(g.replay) == 0 {
		return nil
	}

	ended := g.Ended
	if ended == 0 {
		ended = time.Now().Unix()
	}

	tx, err := begin()
	if err != nil {
		return err
	}
	defer tx.Commit(context.Background())

	_, err = tx.Exec(context.Background(), "INSERT INTO game (variant, started, ended, player1, account1, player2, account2, points, winner, wintype, replay) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)", g.Variant, g.Started, ended, g.allowed1, g.account1, g.allowed2, g.account2, g.Points, g.Winner, winType, bytes.Join(replay, []byte("\n")))
	if err != nil {
		return err
	}

	if g.account1 != 0 {
		_, err = tx.Exec(context.Background(), "UPDATE account SET active = $1 WHERE id = $2", time.Now().Unix(), g.account1)
		if err != nil {
			return err
		}
	}
	if g.account2 != 0 {
		_, err = tx.Exec(context.Background(), "UPDATE account SET active = $1 WHERE id = $2", time.Now().Unix(), g.account2)
		if err != nil {
			return err
		}
	}
	return nil
}

func recordMatchResult(g *serverGame, matchType int) (int, error) {
	dbLock.Lock()
	defer dbLock.Unlock()

	if db == nil || g.Started == 0 || g.Winner == 0 || g.account1 == 0 || g.account2 == 0 || g.account1 == g.account2 {
		return 0, nil
	}

	tx, err := begin()
	if err != nil {
		return 0, err
	}
	defer tx.Commit(context.Background())

	columnName := ratingColumn(matchType, g.Variant, g.Points != 1)

	var rating1i int
	err = tx.QueryRow(context.Background(), "SELECT "+columnName+" FROM account WHERE id = $1", g.account1).Scan(&rating1i)
	if err != nil {
		return 0, err
	}
	rating1 := float64(rating1i) / 100

	var rating2i int
	err = tx.QueryRow(context.Background(), "SELECT "+columnName+" FROM account WHERE id = $1", g.account2).Scan(&rating2i)
	if err != nil {
		return 0, err
	}
	rating2 := float64(rating2i) / 100

	outcome1, outcome2 := 1.0, 0.0
	if g.Winner == 2 {
		outcome1, outcome2 = 0.0, 1.0
	}
	rating1New, _, _ := glicko2.Rank(rating1, 50, 0.06, []glicko2.Opponent{ratingPlayer{rating2, 30, 0.06, outcome1}}, 0.6)
	rating2New, _, _ := glicko2.Rank(rating2, 50, 0.06, []glicko2.Opponent{ratingPlayer{rating1, 30, 0.06, outcome2}}, 0.6)

	active := time.Now().Unix()
	_, err = tx.Exec(context.Background(), "UPDATE account SET "+columnName+" = $1, active = $2 WHERE id = $3", int(rating1New*100), active, g.account1)
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(context.Background(), "UPDATE account SET "+columnName+" = $1, active = $2 WHERE id = $3", int(rating2New*100), active, g.account2)
	if err != nil {
		return 0, err
	}

	if g.client1 != nil && g.client1.account != nil {
		if matchType == matchTypeCasual {
			g.client1.account.casual.setRating(g.Variant, g.Points > 1, int(rating1New*100))
		} else {
			g.client1.account.competitive.setRating(g.Variant, g.Points > 1, int(rating1New*100))
		}
	}
	if g.client2 != nil && g.client2.account != nil {
		if matchType == matchTypeCasual {
			g.client2.account.casual.setRating(g.Variant, g.Points > 1, int(rating2New*100))
		} else {
			g.client2.account.competitive.setRating(g.Variant, g.Points > 1, int(rating2New*100))
		}
	}

	delta := rating1New - rating1
	if delta <= 0 {
		delta = rating2New - rating2
	}
	return int(delta), nil
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

func addBan(ipHash string, account int, staff int, reason string) error {
	dbLock.Lock()
	defer dbLock.Unlock()

	if db == nil || (ipHash == "" && account == 0) {
		return nil
	}

	tx, err := begin()
	if err != nil {
		return err
	}
	defer tx.Commit(context.Background())

	timestamp := time.Now().Unix()

	if ipHash != "" {
		var result int
		err = tx.QueryRow(context.Background(), "SELECT COUNT(*) FROM ban WHERE ip = $1", ipHash).Scan(&result)
		if err != nil {
			log.Fatal(err)
		} else if result == 0 {
			_, err = tx.Exec(context.Background(), "INSERT INTO ban (ip, account, created, staff, reason) VALUES ($1, $2, $3, $4, $5)", ipHash, 0, timestamp, staff, reason)
			if err != nil {
				log.Fatal(err)
			}
		}
	}

	if account != 0 {
		var result int
		err = tx.QueryRow(context.Background(), "SELECT COUNT(*) FROM ban WHERE account = $1", account).Scan(&result)
		if err != nil {
			log.Fatal(err)
		} else if result == 0 {
			_, err = tx.Exec(context.Background(), "INSERT INTO ban (ip, account, created, staff, reason) VALUES ($1, $2, $3, $4, $5)", "", account, timestamp, staff, reason)
			if err != nil {
				log.Fatal(err)
			}
		}
	}
	return nil
}

func checkBan(ipHash string, account int) (bool, string) {
	dbLock.Lock()
	defer dbLock.Unlock()

	if db == nil || (ipHash == "" && account == 0) {
		return false, ""
	}

	tx, err := begin()
	if err != nil {
		return false, ""
	}
	defer tx.Commit(context.Background())

	var row pgx.Row
	if account == 0 {
		row = tx.QueryRow(context.Background(), "SELECT reason FROM ban WHERE ip = $1 LIMIT 1", ipHash)
	} else {
		row = tx.QueryRow(context.Background(), "SELECT reason FROM ban WHERE ip = $1 OR account = $2 LIMIT 1", ipHash, account)
	}
	var reason string
	err = row.Scan(&reason)
	if err != nil {
		return false, ""
	}
	return true, reason
}

func deleteBan(ipHash string, account int) error {
	dbLock.Lock()
	defer dbLock.Unlock()

	if db == nil || (ipHash == "" && account == 0) {
		return nil
	}

	tx, err := begin()
	if err != nil {
		return err
	}
	defer tx.Commit(context.Background())

	if ipHash != "" {
		_, err = tx.Exec(context.Background(), "DELETE FROM ban WHERE ip = $1", ipHash)
		if err != nil {
			log.Fatal(err)
		}
	}

	if account != 0 {
		_, err = tx.Exec(context.Background(), "DELETE FROM ban WHERE account = $1", account)
		if err != nil {
			log.Fatal(err)
		}
	}
	return nil
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
	var winner int8
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

func getLeaderboard(matchType int, variant int8, multiPoint bool) (*leaderboardResult, error) {
	dbLock.Lock()
	defer dbLock.Unlock()

	tx, err := begin()
	if err != nil {
		return nil, err
	}
	defer tx.Commit(context.Background())

	columnName := ratingColumn(matchType, variant, multiPoint)
	ids := make(map[string]int)

	var id int
	result := &leaderboardResult{}
	rows, err := tx.Query(context.Background(), "SELECT id, username, "+columnName+" FROM account ORDER BY "+columnName+" DESC LIMIT 50")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		if err != nil {
			continue
		}
		entry := &leaderboardEntry{}
		err = rows.Scan(&id, &entry.User, &entry.Rating)
		if err != nil {
			continue
		}
		entry.Rating /= 100

		if strings.HasPrefix(entry.User, "bot_") {
			entry.User = "BOT_" + entry.User[4:]
		}

		result.Leaderboard = append(result.Leaderboard, entry)
		ids[entry.User] = id
	}
	if err != nil {
		return nil, err
	}

	pointsCondition := "= 1"
	if multiPoint {
		pointsCondition = "> 1"
	}
	for _, entry := range result.Leaderboard {
		id := ids[entry.User]
		if id == 0 {
			continue
		}
		r2, err := tx.Query(context.Background(), "SELECT COUNT(*) FROM game WHERE ((account1 = $1 AND winner = 1 AND account2 != 0) OR (account2 = $2 AND winner = 2 AND account1 != 0)) AND variant = $3 AND points "+pointsCondition, id, id, variant)
		if err != nil {
			continue
		}
		for r2.Next() {
			if err != nil {
				continue
			}
			err = r2.Scan(&entry.Wins)
		}
		r2, err = tx.Query(context.Background(), "SELECT COUNT(*) FROM game WHERE ((account1 = $1 AND winner = 2 AND account2 != 0) OR (account2 = $2 AND winner = 1 AND account1 != 0)) AND variant = $3 AND points "+pointsCondition, id, id, variant)
		if err != nil {
			continue
		}
		for r2.Next() {
			if err != nil {
				continue
			}
			err = r2.Scan(&entry.Losses)
		}
		if entry.Wins != 0 || entry.Losses != 0 {
			entry.Percent = float64(entry.Wins) / float64(entry.Wins+entry.Losses)
		}

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
	rows, err := tx.Query(context.Background(), "SELECT MIN(started) FROM game")
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
	var games, accounts int
	for {
		rows, err := tx.Query(context.Background(), "SELECT COUNT(*) FROM game WHERE started >= $1 AND started < $2", rangeStart, rangeEnd)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			if err != nil {
				continue
			}
			err = rows.Scan(&games)
		}
		if err != nil {
			return nil, err
		}

		rows, err = tx.Query(context.Background(), "SELECT COUNT(*) FROM account WHERE created >= $1 AND created < $2", rangeStart, rangeEnd)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			if err != nil {
				continue
			}
			err = rows.Scan(&accounts)
		}
		if err != nil {
			return nil, err
		}

		result.History = append(result.History, &serverStatsEntry{
			Date:     time.Unix(rangeStart, 0).Format("2006-01-02"),
			Games:    games,
			Accounts: accounts,
		})

		earliest = earliest.AddDate(0, 0, 1)
		rangeStart, rangeEnd = rangeEnd, earliest.AddDate(0, 0, 1).Unix()
		if rangeStart >= time.Now().Unix() {
			break
		}
	}
	return result, nil
}

func monthlyStats(tz *time.Location) (*serverStatsResult, error) {
	dbLock.Lock()
	defer dbLock.Unlock()

	tx, err := begin()
	if err != nil {
		return nil, err
	}
	defer tx.Commit(context.Background())

	var earliestGame int64
	rows, err := tx.Query(context.Background(), "SELECT MIN(started) FROM game")
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
	m := midnight(time.Unix(earliestGame, 0).In(tz))
	earliest := time.Date(m.Year(), m.Month(), 1, 0, 0, 0, 0, m.Location())
	rangeStart, rangeEnd := earliest.Unix(), earliest.AddDate(0, 1, 0).Unix()
	var games, accounts int
	for {
		rows, err := tx.Query(context.Background(), "SELECT COUNT(*) FROM game WHERE started >= $1 AND started < $2", rangeStart, rangeEnd)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			if err != nil {
				continue
			}
			err = rows.Scan(&games)
		}
		if err != nil {
			return nil, err
		}

		rows, err = tx.Query(context.Background(), "SELECT COUNT(*) FROM account WHERE created >= $1 AND created < $2", rangeStart, rangeEnd)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			if err != nil {
				continue
			}
			err = rows.Scan(&accounts)
		}
		if err != nil {
			return nil, err
		}

		result.History = append(result.History, &serverStatsEntry{
			Date:     time.Unix(rangeStart, 0).Format("2006-01"),
			Games:    games,
			Accounts: accounts,
		})

		earliest = time.Date(earliest.Year(), earliest.Month()+1, 1, 0, 0, 0, 0, m.Location())
		rangeStart, rangeEnd = earliest.Unix(), time.Date(earliest.Year(), earliest.Month()+1, 1, 0, 0, 0, 0, m.Location()).Unix()
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
	rows, err := tx.Query(context.Background(), "SELECT MIN(started) FROM game")
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
	var games, accounts int
	var totalGames, totalAccounts int
	for {
		rows, err := tx.Query(context.Background(), "SELECT COUNT(*) FROM game WHERE started >= $1 AND started < $2", rangeStart, rangeEnd)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			if err != nil {
				continue
			}
			err = rows.Scan(&games)
		}
		if err != nil {
			return nil, err
		}
		totalGames += games

		rows, err = tx.Query(context.Background(), "SELECT COUNT(*) FROM account WHERE created >= $1 AND created < $2", rangeStart, rangeEnd)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			if err != nil {
				continue
			}
			err = rows.Scan(&accounts)
		}
		if err != nil {
			return nil, err
		}
		totalAccounts += accounts

		result.History = append(result.History, &serverStatsEntry{
			Date:     time.Unix(rangeStart, 0).Format("2006-01-02"),
			Games:    totalGames,
			Accounts: totalAccounts,
		})

		earliest = earliest.AddDate(0, 0, 1)
		rangeStart, rangeEnd = rangeEnd, earliest.AddDate(0, 0, 1).Unix()
		if rangeStart >= time.Now().Unix() {
			break
		}
	}
	return result, nil
}

func accountStats(name string, matchType int, variant int8, tz *time.Location) (*accountStatsResult, error) {
	dbLock.Lock()
	defer dbLock.Unlock()

	tx, err := begin()
	if err != nil {
		return nil, err
	}
	defer tx.Commit(context.Background())

	var earliestGame int64
	rows, err := tx.Query(context.Background(), "SELECT MIN(started) FROM game WHERE (player1 = $1 OR player2 = $2) AND variant = $3", name, name, variant)
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

	result := &accountStatsResult{}
	m := midnight(time.Unix(earliestGame, 0).In(tz))
	earliest := time.Date(m.Year(), m.Month(), 1, 0, 0, 0, 0, m.Location())
	rangeStart, rangeEnd := earliest.Unix(), earliest.AddDate(0, 1, -(earliest.Day()-1)).Unix()
	var winCount, lossCount int
	for {
		rows, err := tx.Query(context.Background(), "SELECT COUNT(*) FROM game WHERE started >= $1 AND started < $2 AND (player1 = $3 OR player2 = $4) AND variant = $5", rangeStart, rangeEnd, name, name, variant)
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

		rows, err = tx.Query(context.Background(), "SELECT COUNT(*) FROM game WHERE started >= $1 AND started < $2 AND ((player1 = $3 AND winner = 1) OR (player2 = $4 AND winner = 2)) AND variant = $5", rangeStart, rangeEnd, name, name, variant)
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
			result.History = append(result.History, &accountStatsEntry{
				Date:    time.Unix(rangeStart, 0).Format("2006-01"),
				Percent: float64(winCount) / float64(winCount+lossCount),
				Wins:    winCount,
				Losses:  lossCount,
			})
		}

		earliest = time.Date(earliest.Year(), earliest.Month()+1, 1, 0, 0, 0, 0, m.Location())
		rangeStart, rangeEnd = earliest.Unix(), time.Date(earliest.Year(), earliest.Month()+1, 1, 0, 0, 0, 0, m.Location()).Unix()
		if rangeStart >= time.Now().Unix() {
			break
		}
	}
	return result, nil
}

func playerVsPlayerStats(tz *time.Location) (*serverStatsResult, error) {
	dbLock.Lock()
	defer dbLock.Unlock()

	tx, err := begin()
	if err != nil {
		return nil, err
	}
	defer tx.Commit(context.Background())

	condition := "player1 NOT LIKE 'Guest_%' AND player1 NOT LIKE 'BOT_%' AND player2 NOT LIKE 'Guest_%' AND player2 NOT LIKE 'BOT_%'"

	var earliestGame int64
	rows, err := tx.Query(context.Background(), "SELECT MIN(started) FROM game WHERE "+condition)
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
	m := midnight(time.Unix(earliestGame, 0).In(tz))
	earliest := time.Date(m.Year(), m.Month(), 1, 0, 0, 0, 0, m.Location())
	rangeStart, rangeEnd := earliest.Unix(), earliest.AddDate(0, 1, 0).Unix()
	var games int
	for {
		rows, err := tx.Query(context.Background(), "SELECT COUNT(*) FROM game WHERE started >= $1 AND started < $2 AND "+condition, rangeStart, rangeEnd)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			if err != nil {
				continue
			}
			err = rows.Scan(&games)
		}
		if err != nil {
			return nil, err
		}

		result.History = append(result.History, &serverStatsEntry{
			Date:  time.Unix(rangeStart, 0).Format("2006-01"),
			Games: games,
		})

		earliest = time.Date(earliest.Year(), earliest.Month()+1, 1, 0, 0, 0, 0, m.Location())
		rangeStart, rangeEnd = earliest.Unix(), time.Date(earliest.Year(), earliest.Month()+1, 1, 0, 0, 0, 0, m.Location()).Unix()
		if rangeStart >= time.Now().Unix() {
			break
		}
	}
	return result, nil
}

func ratingColumn(matchType int, variant int8, multiPoint bool) string {
	var columnStart = "casual_"
	if matchType == matchTypeRated {
		columnStart = "rated_"
	}

	var columnMid string
	switch variant {
	case bgammon.VariantBackgammon:
		columnMid = "backgammon_"
	case bgammon.VariantAceyDeucey:
		columnMid = "acey_"
	case bgammon.VariantTabula:
		columnMid = "tabula_"
	default:
		log.Panicf("unknown variant: %d", variant)
	}

	columnEnd := "single"
	if multiPoint {
		columnEnd = "multi"
	}

	return columnStart + columnMid + columnEnd
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
	Date: ` + time.Now().Format(time.RFC1123Z) + `
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
Date: `+time.Now().Format(time.RFC1123Z)+`
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

type ratingPlayer struct {
	r       float64
	rd      float64
	sigma   float64
	outcome float64
}

func (p ratingPlayer) R() float64 {
	return p.r
}

func (p ratingPlayer) RD() float64 {
	return p.rd
}

func (p ratingPlayer) Sigma() float64 {
	return p.sigma
}

func (p ratingPlayer) SJ() float64 {
	return p.outcome
}
