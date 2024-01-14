package server

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"time"

	"code.rocket9labs.com/tslocum/bgammon"
)

type serverGame struct {
	id         int
	created    int64
	active     int64
	name       []byte
	password   []byte
	client1    *serverClient
	client2    *serverClient
	spectators []*serverClient
	allowed1   []byte
	allowed2   []byte
	account1   int
	account2   int
	forefeit   int8
	rematch    int8
	rejoin1    bool
	rejoin2    bool
	replay     [][]byte
	*bgammon.Game
}

func newServerGame(id int, variant int8) *serverGame {
	now := time.Now().Unix()
	return &serverGame{
		id:      id,
		created: now,
		active:  now,
		Game:    bgammon.NewGame(variant),
	}
}

func (g *serverGame) playForcedMoves() bool {
	if g.Winner != 0 || len(g.Moves) != 0 || g.client1 == nil || g.client2 == nil {
		return false
	}
	rolls := g.DiceRolls()
	if len(rolls) == 0 {
		return false
	}
	var playerName string
	switch g.Turn {
	case 1:
		if !g.client1.autoplay {
			return false
		}
		playerName = g.Player1.Name
	case 2:
		if !g.client2.autoplay {
			return false
		}
		playerName = g.Player2.Name
	case 0:
		return false
	}
	tb, ok := g.TabulaBoard()
	if !ok {
		return false
	}
	allMoves, _ := tb.Available(g.Turn)
	if len(allMoves) == 0 {
		return false
	}
	var forcedMoves [][2]int8
	if len(allMoves) == 1 {
		for i := range allMoves {
			for j := 0; j < 4; j++ {
				if allMoves[i][j][0] == 0 && allMoves[i][j][1] == 0 {
					break
				}
				forcedMoves = append(forcedMoves, allMoves[i][j])
			}
		}
	} else {
	FORCEDMOVES:
		for _, m1 := range allMoves[0] {
			for i := range allMoves {
				if i == 0 {
					continue
				}
				var found bool
				for j := 0; j < 4; j++ {
					if allMoves[i][j][0] == 0 && allMoves[i][j][1] == 0 {
						break
					} else if allMoves[i][j][0] == m1[0] && allMoves[i][j][1] == m1[1] {
						found = true
						break
					}
				}
				if !found {
					continue FORCEDMOVES
				}
			}
			forcedMoves = append(forcedMoves, m1)
		}
	}
	if len(forcedMoves) == 0 {
		return false
	}
	g.eachClient(func(client *serverClient) {
		g.sendBoard(client, true)
	})
	for _, move := range forcedMoves {
		if g.HaveDiceRoll(move[0], move[1]) == 0 {
			break
		}
		ok, _ := g.AddMoves([][]int8{{move[0], move[1]}}, false)
		if !ok {
			log.Fatalf("failed to play forced move %v: %v %v (%v) (%v)", move, forcedMoves, g.DiceRolls(), g.Game, g.Board)
		}
		g.eachClient(func(client *serverClient) {
			ev := &bgammon.EventMoved{
				Moves: bgammon.FlipMoves([][]int8{{move[0], move[1]}}, client.playerNumber, g.Variant),
			}
			ev.Player = playerName
			client.sendEvent(ev)
		})
		if g.handleWin() {
			return true
		}
	}
	return true
}

func (g *serverGame) roll(player int8) bool {
	if g.client1 == nil || g.client2 == nil || g.Winner != 0 {
		return false
	}

	if g.Turn == 0 {
		if player == 1 {
			if g.Roll1 != 0 {
				return false
			}
			g.Roll1 = int8(RandInt(6) + 1)
		} else {
			if g.Roll2 != 0 {
				return false
			}
			g.Roll2 = int8(RandInt(6) + 1)
		}

		// Only allow the same players to rejoin the game.
		if g.allowed1 == nil {
			g.allowed1, g.allowed2 = g.client1.name, g.client2.name
		}

		// Store account IDs.
		if g.Started.IsZero() && g.Roll1 != 0 && g.Roll2 != 0 {
			g.Started = time.Now()
			if g.client1.account != nil {
				g.account1 = g.client1.account.id
			}
			if g.client2.account != nil {
				g.account2 = g.client2.account.id
			}
		}
		return true
	} else if player != g.Turn || g.Roll1 != 0 || g.Roll2 != 0 {
		return false
	}

	g.Roll1 = int8(RandInt(6) + 1)
	g.Roll2 = int8(RandInt(6) + 1)
	if g.Variant == bgammon.VariantTabula {
		g.Roll3 = int8(RandInt(6) + 1)
	}

	return true
}

func (g *serverGame) sendBoard(client *serverClient, forcedMove bool) {
	if client.json {
		ev := &bgammon.EventBoard{
			GameState: bgammon.GameState{
				Game:         g.Game,
				PlayerNumber: client.playerNumber,
				Available:    g.LegalMoves(false),
				Forced:       forcedMove,
				Spectating:   g.client1 != client && g.client2 != client,
			},
		}

		// Reverse spaces for white.
		if client.playerNumber == 2 {
			ev.GameState.Game = ev.GameState.Copy(true)

			ev.GameState.PlayerNumber = 1
			ev.GameState.Player1, ev.GameState.Player2 = ev.GameState.Player2, ev.GameState.Player1
			ev.GameState.Player1.Number = 1
			ev.GameState.Player2.Number = 2

			switch ev.GameState.Turn {
			case 1:
				ev.GameState.Turn = 2
			case 2:
				ev.GameState.Turn = 1
			}

			switch ev.GameState.DoublePlayer {
			case 1:
				ev.GameState.DoublePlayer = 2
			case 2:
				ev.GameState.DoublePlayer = 1
			}

			switch ev.GameState.Winner {
			case 1:
				ev.GameState.Winner = 2
			case 2:
				ev.GameState.Winner = 1
			}

			if ev.GameState.Roll1 == 0 || ev.GameState.Roll2 == 0 {
				ev.GameState.Roll1, ev.GameState.Roll2 = ev.GameState.Roll2, ev.GameState.Roll1
			}

			// Flip board.
			if g.Variant == bgammon.VariantTabula {
				for space := int8(1); space <= 24; space++ {
					ev.Board[space] = g.Game.Board[space] * -1
				}
			} else {
				for space := int8(1); space <= 24; space++ {
					ev.Board[space] = g.Game.Board[bgammon.FlipSpace(space, client.playerNumber, g.Variant)] * -1
				}
			}
			ev.Board[bgammon.SpaceHomePlayer], ev.Board[bgammon.SpaceHomeOpponent] = ev.Board[bgammon.SpaceHomeOpponent]*-1, ev.Board[bgammon.SpaceHomePlayer]*-1
			ev.Board[bgammon.SpaceBarPlayer], ev.Board[bgammon.SpaceBarOpponent] = ev.Board[bgammon.SpaceBarOpponent]*-1, ev.Board[bgammon.SpaceBarPlayer]*-1
			ev.Moves = bgammon.FlipMoves(g.Game.Moves, client.playerNumber, g.Variant)
			ev.GameState.Available = g.LegalMoves(false)
			for i := range ev.GameState.Available {
				ev.GameState.Available[i][0], ev.GameState.Available[i][1] = bgammon.FlipSpace(ev.GameState.Available[i][0], client.playerNumber, g.Variant), bgammon.FlipSpace(ev.GameState.Available[i][1], client.playerNumber, g.Variant)
			}
		}

		// Sort available moves.
		bgammon.SortMoves(ev.Available)

		client.sendEvent(ev)
		return
	}

	scanner := bufio.NewScanner(bytes.NewReader(g.BoardState(client.playerNumber, false)))
	for scanner.Scan() {
		client.sendNotice(string(scanner.Bytes()))
	}
}

func (g *serverGame) playerCount() int8 {
	var c int8
	if g.client1 != nil {
		c++
	}
	if g.client2 != nil {
		c++
	}
	return c
}

func (g *serverGame) eachClient(f func(client *serverClient)) {
	if g.client1 != nil {
		f(g.client1)
	}
	if g.client2 != nil {
		f(g.client2)
	}
	for _, spectator := range g.spectators {
		f(spectator)
	}
}

func (g *serverGame) addClient(client *serverClient) (spectator bool) {
	if g.allowed1 != nil && !bytes.Equal(client.name, g.allowed1) && !bytes.Equal(client.name, g.allowed2) {
		spectator = true
	} else if g.client1 != nil && g.client2 != nil {
		spectator = true
	}
	if spectator {
		for _, spec := range g.spectators {
			if spec == client {
				return true
			}
		}
		client.playerNumber = 1
		g.spectators = append(g.spectators, client)
		ev := &bgammon.EventJoined{
			GameID:       g.id,
			PlayerNumber: 1,
		}
		ev.Player = string(client.name)
		client.sendEvent(ev)
		g.sendBoard(client, false)
		return spectator
	}

	var playerNumber int8
	defer func() {
		ev := &bgammon.EventJoined{
			GameID:       g.id,
			PlayerNumber: 1,
		}
		ev.Player = string(client.name)
		client.sendEvent(ev)
		g.sendBoard(client, false)

		if playerNumber == 0 {
			return
		}

		opponent := g.opponent(client)
		if opponent != nil {
			ev := &bgammon.EventJoined{
				GameID:       g.id,
				PlayerNumber: 2,
			}
			ev.Player = string(client.name)
			opponent.sendEvent(ev)
			g.sendBoard(opponent, false)
		}

		{
			ev := &bgammon.EventJoined{
				GameID:       g.id,
				PlayerNumber: client.playerNumber,
			}
			ev.Player = string(client.name)
			for _, spectator := range g.spectators {
				spectator.sendEvent(ev)
				g.sendBoard(spectator, false)
			}
		}

		if playerNumber == 1 {
			g.rejoin1 = true
		} else {
			g.rejoin2 = true
		}

		if g.forefeit == playerNumber {
			g.forefeit = 0
		}
	}()
	var rating int
	if client.account != nil {
		rating = client.account.casual.getRating(g.Variant, g.Points > 1) / 100
	}
	switch {
	case g.client1 != nil:
		g.client2 = client
		g.Player2.Name = string(client.name)
		g.Player2.Rating = rating
		client.playerNumber = 2
		playerNumber = 2
	case g.client2 != nil:
		g.client1 = client
		g.Player1.Name = string(client.name)
		g.Player1.Rating = rating
		client.playerNumber = 1
		playerNumber = 1
	default:
		if RandInt(2) == 0 {
			g.client1 = client
			g.Player1.Name = string(client.name)
			g.Player1.Rating = rating
			client.playerNumber = 1
			playerNumber = 1
		} else {
			g.client2 = client
			g.Player2.Name = string(client.name)
			g.Player2.Rating = rating
			client.playerNumber = 2
			playerNumber = 2
		}
	}
	return spectator
}

func (g *serverGame) removeClient(client *serverClient) {
	var playerNumber int
	defer func() {
		if playerNumber == 0 {
			return
		}

		ev := &bgammon.EventLeft{}
		ev.Player = string(client.name)

		client.sendEvent(ev)
		if !client.json {
			g.sendBoard(client, false)
		}

		var opponent *serverClient
		if playerNumber == 1 && g.client2 != nil {
			opponent = g.client2
		} else if playerNumber == 2 && g.client1 != nil {
			opponent = g.client1
		}
		if opponent != nil {
			opponent.sendEvent(ev)
			if !opponent.json {
				g.sendBoard(opponent, false)
			}
		}

		for _, spectator := range g.spectators {
			spectator.sendEvent(ev)
			if !spectator.json {
				g.sendBoard(spectator, false)
			}
		}

		if playerNumber == 1 && g.client2 != nil {
			g.forefeit = 1
		} else if playerNumber == 2 && g.client1 != nil {
			g.forefeit = 2
		}

		client.playerNumber = 0
	}()
	switch {
	case g.client1 == client:
		g.client1 = nil
		g.Player1.Name = ""
		g.Player1.Rating = 0
		playerNumber = 1
	case g.client2 == client:
		g.client2 = nil
		g.Player2.Name = ""
		g.Player2.Rating = 0
		playerNumber = 2
	default:
		for i, spectator := range g.spectators {
			if spectator == client {
				g.spectators = append(g.spectators[:i], g.spectators[i+1:]...)

				ev := &bgammon.EventLeft{}
				ev.Player = string(client.name)

				client.sendEvent(ev)
				if !client.json {
					g.sendBoard(client, false)
				}

				client.playerNumber = 0
				return
			}
		}
		return
	}
}

func (g *serverGame) opponent(client *serverClient) *serverClient {
	if g.client1 == client {
		return g.client2
	} else if g.client2 == client {
		return g.client1
	}
	return nil
}

func (g *serverGame) listing(playerName []byte) *bgammon.GameListing {
	if g.terminated() {
		return nil
	}

	var playerCount int8
	if len(g.allowed1) != 0 && (len(playerName) == 0 || (!bytes.Equal(g.allowed1, playerName) && !bytes.Equal(g.allowed2, playerName))) {
		playerCount = 2
	} else {
		playerCount = g.playerCount()
	}

	var rating int
	if g.client1 != nil && g.client1.account != nil {
		rating = g.client1.account.casual.getRating(g.Variant, g.Points > 1)
	}
	if g.client2 != nil && g.client2.account != nil {
		r := g.client2.account.casual.getRating(g.Variant, g.Points > 1)
		if r > rating {
			rating = r
		}
	}

	name := string(g.name)
	switch g.Variant {
	case bgammon.VariantAceyDeucey:
		name = "(Acey-deucey) " + name
	case bgammon.VariantTabula:
		name = "(Tabula) " + name
	}

	return &bgammon.GameListing{
		ID:       g.id,
		Points:   g.Points,
		Password: len(g.password) != 0,
		Players:  playerCount,
		Rating:   rating / 100,
		Name:     name,
	}
}

func (g *serverGame) recordEvent() {
	r1, r2, r3 := g.Roll1, g.Roll2, g.Roll3
	if r2 > r1 {
		r1, r2 = r2, r1
	}
	if r3 > r1 {
		r1, r3 = r3, r1
	}
	if r3 > r2 {
		r2, r3 = r3, r2
	}
	var movesFormatted []byte
	if len(g.Moves) != 0 {
		movesFormatted = append([]byte(" "), bgammon.FormatMoves(g.Moves)...)
	}
	line := []byte(fmt.Sprintf("%d r %d-%d", g.Turn, r1, r2))
	if r3 > 0 {
		line = append(line, []byte(fmt.Sprintf("-%d", r3))...)
	}
	line = append(line, movesFormatted...)
	g.replay = append(g.replay, line)
}

func (g *serverGame) nextTurn(reroll bool) {
	g.Game.NextTurn(reroll)
	if reroll {
		return
	}

	// Roll automatically.
	if g.Winner == 0 {
		gameState := &bgammon.GameState{
			Game:         g.Game,
			PlayerNumber: g.Turn,
			Available:    g.LegalMoves(false),
		}
		if !gameState.MayDouble() {
			if !g.roll(g.Turn) {
				g.eachClient(func(client *serverClient) {
					client.Terminate("Server error")
				})
				return
			}
			ev := &bgammon.EventRolled{
				Roll1: g.Roll1,
				Roll2: g.Roll2,
				Roll3: g.Roll3,
			}
			if g.Turn == 1 {
				ev.Player = gameState.Player1.Name
			} else {
				ev.Player = gameState.Player2.Name
			}
			g.eachClient(func(client *serverClient) {
				client.sendEvent(ev)
			})

			// Play forced moves automatically.
			forcedMove := g.playForcedMoves()
			if forcedMove && len(g.LegalMoves(false)) == 0 {
				chooseRoll := g.Variant == bgammon.VariantAceyDeucey && ((g.Roll1 == 1 && g.Roll2 == 2) || (g.Roll1 == 2 && g.Roll2 == 1)) && len(g.Moves) == 2
				if g.Variant != bgammon.VariantAceyDeucey || !chooseRoll {
					g.recordEvent()
					g.nextTurn(false)
					return
				}
			}
		}
	}

	g.eachClient(func(client *serverClient) {
		g.sendBoard(client, false)
	})
}

func (g *serverGame) handleWin() bool {
	if g.Winner == 0 {
		return false
	}
	var opponent int8 = 1
	opponentHome := bgammon.SpaceHomePlayer
	opponentEntered := g.Player1.Entered
	playerBar := bgammon.SpaceBarPlayer
	if g.Winner == 1 {
		opponent = 2
		opponentHome = bgammon.SpaceHomeOpponent
		opponentEntered = g.Player2.Entered
		playerBar = bgammon.SpaceBarOpponent
	}

	backgammon := bgammon.PlayerCheckers(g.Board[playerBar], opponent) != 0
	if !backgammon {
		homeStart, homeEnd := bgammon.HomeRange(g.Winner, g.Variant)
		bgammon.IterateSpaces(homeStart, homeEnd, g.Variant, func(space int8, spaceCount int8) {
			if bgammon.PlayerCheckers(g.Board[space], opponent) != 0 {
				backgammon = true
			}
		})
	}

	var winPoints int8
	switch g.Variant {
	case bgammon.VariantAceyDeucey:
		for space := int8(0); space < bgammon.BoardSpaces; space++ {
			if (space == bgammon.SpaceHomePlayer || space == bgammon.SpaceHomeOpponent) && opponentEntered {
				continue
			}
			winPoints += bgammon.PlayerCheckers(g.Board[space], opponent)
		}
	case bgammon.VariantTabula:
		winPoints = 1
	default:
		if backgammon {
			winPoints = 3 // Award backgammon.
		} else if g.Board[opponentHome] == 0 {
			winPoints = 2 // Award gammon.
		} else {
			winPoints = 1
		}
	}

	g.replay = append([][]byte{[]byte(fmt.Sprintf("i %d %s %s %d %d %d %d %d %d", g.Started.Unix(), g.Player1.Name, g.Player2.Name, g.Points, g.Player1.Points, g.Player2.Points, g.Winner, winPoints, g.Variant))}, g.replay...)

	r1, r2, r3 := g.Roll1, g.Roll2, g.Roll3
	if r2 > r1 {
		r1, r2 = r2, r1
	}
	if r3 > r1 {
		r1, r3 = r3, r1
	}
	if r3 > r2 {
		r2, r3 = r3, r2
	}
	var movesFormatted []byte
	if len(g.Moves) != 0 {
		movesFormatted = append([]byte(" "), bgammon.FormatMoves(g.Moves)...)
	}
	line := []byte(fmt.Sprintf("%d r %d-%d", g.Turn, r1, r2))
	if r3 > 0 {
		line = append(line, []byte(fmt.Sprintf("-%d", r3))...)
	}
	line = append(line, movesFormatted...)
	g.replay = append(g.replay, line)

	winEvent := &bgammon.EventWin{
		Points: winPoints * g.DoubleValue,
	}
	var reset bool
	if g.Winner == 1 {
		winEvent.Player = g.Player1.Name
		g.Player1.Points = g.Player1.Points + winPoints*g.DoubleValue
		if g.Player1.Points < g.Points {
			reset = true
		} else {
			g.Ended = time.Now()
		}
	} else {
		winEvent.Player = g.Player2.Name
		g.Player2.Points = g.Player2.Points + winPoints*g.DoubleValue
		if g.Player2.Points < g.Points {
			reset = true
		} else {
			g.Ended = time.Now()
		}
	}

	winType := winPoints
	if g.Variant != bgammon.VariantBackgammon {
		winType = 1
	}
	err := recordGameResult(g, winType, g.replay)
	if err != nil {
		log.Fatalf("failed to record game result: %s", err)
	}

	if !reset {
		err := recordMatchResult(g, matchTypeCasual)
		if err != nil {
			log.Fatalf("failed to record match result: %s", err)
		}
	} else {
		g.Reset()
		g.replay = g.replay[:0]
	}
	g.eachClient(func(client *serverClient) {
		client.sendEvent(winEvent)
	})

	if g.client1 != nil && g.client1.account != nil {
		g.Player1.Rating = g.client1.account.casual.getRating(g.Variant, g.Points > 1) / 100
	}
	if g.client2 != nil && g.client2.account != nil {
		g.Player2.Rating = g.client2.account.casual.getRating(g.Variant, g.Points > 1) / 100
	}
	g.eachClient(func(client *serverClient) {
		g.sendBoard(client, false)
	})
	return true
}

func (g *serverGame) terminated() bool {
	return g.client1 == nil && g.client2 == nil
}
