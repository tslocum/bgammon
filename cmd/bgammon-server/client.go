package main

import "code.rocket9labs.com/tslocum/bgammon"

type serverClient struct {
	id         int
	name       []byte
	account    int
	connected  int64
	lastActive int64
	commands   <-chan []byte
	events     chan<- []byte
	bgammon.Client
}
