package main

import (
	"code.rocket9labs.com/tslocum/bgammon"
	"time"
)

type serverGame struct {
	id         int
	created    int64
	lastActive int64
	name       []byte
	password   []byte
	client1    bgammon.Client
	client2    bgammon.Client
	*bgammon.Game
}

func newServerGame(id int) *serverGame {
	now := time.Now().Unix()
	return &serverGame{
		id:         id,
		created:    now,
		lastActive: now,
		Game:       bgammon.NewGame(),
	}
}
