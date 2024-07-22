//go:build !full

package server

import "log"

func (s *server) Listen(network string, address string) {
	log.Fatal("bgammon-server was built without the 'full' tag. Only local connections are possible.")
}
