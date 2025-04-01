package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"

	"codeberg.org/tslocum/bgammon/pkg/server"
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
	flag.BoolVar(&rollStatistics, "dice-stats", false, "print dice roll statistics and exit")
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
		fmt.Println(server.DiceStats())
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
