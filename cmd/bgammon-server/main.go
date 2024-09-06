package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"

	"code.rocket9labs.com/tslocum/bgammon/pkg/server"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

func main() {
	var (
		tcpAddress     string
		wsAddress      string
		tz             string
		dataSource     string
		mailServer     string
		passwordSalt   string
		resetSalt      string
		ipSalt         string
		verbose        bool
		debug          int
		debugCommands  bool
		rollStatistics bool
	)
	flag.StringVar(&tcpAddress, "tcp", "localhost:1337", "TCP listen address")
	flag.StringVar(&wsAddress, "ws", "", "WebSocket listen address")
	flag.StringVar(&tz, "tz", "", "Time zone used when calculating statistics")
	flag.StringVar(&dataSource, "db", "", "Database data source (postgres://username:password@localhost:5432/database_name")
	flag.StringVar(&mailServer, "smtp", "", "SMTP server address")
	flag.BoolVar(&verbose, "verbose", false, "Print all client messages")
	flag.IntVar(&debug, "debug", 0, "print debug information and serve pprof on specified port")
	flag.BoolVar(&debugCommands, "debug-commands", false, "allow players to use restricted commands")
	flag.BoolVar(&rollStatistics, "statistics", false, "print dice roll statistics and exit")
	flag.Parse()

	if dataSource == "" {
		dataSource = os.Getenv("BGAMMON_DB")
	}

	if mailServer == "" {
		mailServer = os.Getenv("BGAMMON_SMTP")
	}

	passwordSalt = os.Getenv("BGAMMON_SALT_PASSWORD")
	resetSalt = os.Getenv("BGAMMON_SALT_RESET")
	ipSalt = os.Getenv("BGAMMON_SALT_IP")

	certDomain := os.Getenv("BGAMMON_CERT_DOMAIN")
	certFolder := os.Getenv("BGAMMON_CERT_FOLDER")
	certEmail := os.Getenv("BGAMMON_CERT_EMAIL")
	certAddress := os.Getenv("BGAMMON_CERT_ADDRESS")

	if rollStatistics {
		printRollStatistics()
		return
	}

	if tcpAddress == "" && wsAddress == "" {
		log.Fatal("Error: A TCP and/or WebSocket listen address must be specified.")
	}

	if debug > 0 {
		go func() {
			log.Fatal(http.ListenAndServe(fmt.Sprintf("localhost:%d", debug), nil))
		}()
	}

	s := server.NewServer(tz, dataSource, mailServer, passwordSalt, resetSalt, ipSalt, certDomain, certFolder, certEmail, certAddress, false, verbose || debug > 0, debugCommands)
	if tcpAddress != "" {
		s.Listen("tcp", tcpAddress)
	}
	if wsAddress != "" {
		s.Listen("ws", wsAddress)
	}
	select {}
}

func printRollStatistics() {
	var oneSame, doubles int
	var lastroll1, lastroll2 int
	var rolls [6]int

	const total = 10000000
	for i := 0; i < total; i++ {
		roll1 := server.RandInt(6) + 1
		roll2 := server.RandInt(6) + 1

		rolls[roll1-1]++
		rolls[roll2-1]++

		if roll1 == lastroll1 || roll1 == lastroll2 || roll2 == lastroll1 || roll2 == lastroll2 {
			oneSame++
		}

		if roll1 == roll2 {
			doubles++
		}

		lastroll1, lastroll2 = roll1, roll2
	}

	p := message.NewPrinter(language.English)
	p.Printf("Rolled %d pairs of dice.\nDoubles: %d (%.0f%%). One same as last: %d (%.0f%%).\n1s: %d (%.0f%%), 2s: %d (%.0f%%), 3s: %d (%.0f%%), 4s: %d (%.0f%%), 5s: %d (%.0f%%), 6s: %d (%.0f%%).\n", total, doubles, float64(doubles)/float64(total)*100, oneSame, float64(oneSame)/float64(total)*100, rolls[0], float64(rolls[0])/float64(total*2)*100, rolls[1], float64(rolls[1])/float64(total*2)*100, rolls[2], float64(rolls[2])/float64(total*2)*100, rolls[3], float64(rolls[3])/float64(total*2)*100, rolls[4], float64(rolls[4])/float64(total*2)*100, rolls[5], float64(rolls[5])/float64(total*2)*100)
}
