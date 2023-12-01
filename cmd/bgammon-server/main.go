package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"

	"code.rocket9labs.com/tslocum/bgammon/pkg/server"
)

func main() {
	var (
		tcpAddress     string
		wsAddress      string
		tz             string
		dataSource     string
		debug          int
		debugCommands  bool
		rollStatistics bool
	)
	flag.StringVar(&tcpAddress, "tcp", "localhost:1337", "TCP listen address")
	flag.StringVar(&wsAddress, "ws", "localhost:1338", "WebSocket listen address")
	flag.StringVar(&tz, "tz", "", "Time zone used when calculating statistics")
	flag.StringVar(&dataSource, "db", "", "Database data source (postgres://username:password@localhost:5432/database_name")
	flag.IntVar(&debug, "debug", 0, "print debug information and serve pprof on specified port")
	flag.BoolVar(&debugCommands, "debug-commands", false, "allow players to use restricted commands")
	flag.BoolVar(&rollStatistics, "statistics", false, "print dice roll statistics and exit")
	flag.Parse()

	if dataSource == "" {
		dataSource = os.Getenv("BGAMMON_DB")
	}

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

	s := server.NewServer(tz, dataSource, false, debugCommands)
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

	total := 1000000
	for i := 0; i < total; i++ {
		roll1 := server.RandInt(6) + 1
		roll2 := server.RandInt(6) + 1

		if roll1 == lastroll1 || roll1 == lastroll2 || roll2 == lastroll1 || roll2 == lastroll2 {
			oneSame++
		}

		if roll1 == roll2 {
			doubles++
		}

		lastroll1, lastroll2 = roll1, roll2
	}

	log.Printf("total: %d, one same: %d (%.0f%%), doubles: %d (%.0f%%)", total, oneSame, float64(oneSame)/float64(total)*100, doubles, float64(doubles)/float64(total)*100)
}
