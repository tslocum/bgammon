package main

import (
	"log"

	"code.rocket9labs.com/tslocum/bgammon"
)

func main() {
	b := bgammon.NewBoard()
	log.Printf("%+v", b)
}
