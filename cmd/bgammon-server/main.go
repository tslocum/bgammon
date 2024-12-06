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
	op := &server.Options{}
	var (
		tcpAddress     string
		wsAddress      string
		debugPort      int
		debugCommands  bool
		rollStatistics bool
	)
	flag.StringVar(&tcpAddress, "tcp", "localhost:1337", "TCP listen address")
	flag.StringVar(&wsAddress, "ws", "", "WebSocket listen address")
	flag.StringVar(&op.TZ, "tz", "", "Time zone used when calculating statistics")
	flag.StringVar(&op.DataSource, "db", "", "Database data source (postgres://username:password@localhost:5432/database_name")
	flag.StringVar(&op.MailServer, "smtp", "", "SMTP server address")
	flag.BoolVar(&op.Verbose, "verbose", false, "Print all client messages")
	flag.IntVar(&debugPort, "debug", 0, "print debug information and serve pprof on specified port")
	flag.BoolVar(&debugCommands, "debug-commands", false, "allow players to use restricted commands")
	flag.BoolVar(&rollStatistics, "statistics", false, "print dice roll statistics and exit")
	flag.Parse()

	if debugPort > 0 {
		op.Debug = true
		op.Verbose = true
	}

	if op.DataSource == "" {
		op.DataSource = os.Getenv("BGAMMON_DB")
	}

	if op.MailServer == "" {
		op.MailServer = os.Getenv("BGAMMON_SMTP")
	}

	op.ResetSalt = os.Getenv("BGAMMON_SALT_RESET")
	op.PasswordSalt = os.Getenv("BGAMMON_SALT_PASSWORD")
	op.IPAddressSalt = os.Getenv("BGAMMON_SALT_IP")

	op.CertFile = os.Getenv("BGAMMON_CERT_FILE")
	op.CertKey = os.Getenv("BGAMMON_CERT_KEY")

	if rollStatistics {
		printRollStatistics()
		return
	}

	if tcpAddress == "" && wsAddress == "" {
		log.Fatal("Error: A TCP and/or WebSocket listen address must be specified.")
	}

	if wsAddress != "" && (op.CertFile == "" || op.CertKey == "") {
		log.Fatal("Error: Certificate file and key must be specified to listen for WebSocket connections.")
	}

	if debugPort > 0 {
		go func() {
			log.Fatal(http.ListenAndServe(fmt.Sprintf("localhost:%d", debugPort), nil))
		}()
	}

	s := server.NewServer(op)
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
