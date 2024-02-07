package server

import (
	"bytes"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"code.rocket9labs.com/tslocum/bgammon"
)

func (s *server) handleCommands() {
	var cmd serverCommand
COMMANDS:
	for cmd = range s.commands {
		if cmd.client == nil {
			log.Panicf("nil client with command %s", cmd.command)
		} else if cmd.client.terminating || cmd.client.Terminated() {
			continue
		}

		cmd.command = bytes.TrimSpace(cmd.command)

		firstSpace := bytes.IndexByte(cmd.command, ' ')
		var keyword string
		var startParameters int
		if firstSpace == -1 {
			keyword = string(cmd.command)
			startParameters = len(cmd.command)
		} else {
			keyword = string(cmd.command[:firstSpace])
			startParameters = firstSpace + 1
		}
		if keyword == "" {
			continue
		}
		keyword = strings.ToLower(keyword)
		params := bytes.Fields(cmd.command[startParameters:])

		// Require users to send login command first.
		if cmd.client.accountID == -1 {
			resetCommand := keyword == bgammon.CommandResetPassword
			if resetCommand {
				if len(params) > 0 {
					email := bytes.ToLower(bytes.TrimSpace(params[0]))
					if len(email) > 0 {
						err := resetAccount(s.mailServer, s.resetSalt, email)
						if err != nil {
							log.Fatalf("failed to reset password: %s", err)
						}
					}
				}
				cmd.client.Terminate("resetpasswordok")
				continue
			}

			loginCommand := keyword == bgammon.CommandLogin || keyword == bgammon.CommandLoginJSON || keyword == "lj"
			registerCommand := keyword == bgammon.CommandRegister || keyword == bgammon.CommandRegisterJSON || keyword == "rj"
			if loginCommand || registerCommand {
				if keyword == bgammon.CommandLoginJSON || keyword == bgammon.CommandRegisterJSON || keyword == "lj" || keyword == "rj" {
					cmd.client.json = true
				}

				var username []byte
				var password []byte
				var randomUsername bool
				if registerCommand {
					sendUsage := func() {
						cmd.client.Terminate("Please enter an email, username and password.")
					}

					var email []byte
					if keyword == bgammon.CommandRegisterJSON || keyword == "rj" {
						if len(params) < 4 {
							sendUsage()
							continue
						}
						email = params[1]
						username = params[2]
						password = bytes.Join(params[3:], []byte("_"))
					} else {
						if len(params) < 3 {
							sendUsage()
							continue
						}
						email = params[0]
						username = params[1]
						password = bytes.Join(params[2:], []byte("_"))
					}
					if onlyNumbers.Match(username) {
						cmd.client.Terminate("Failed to register: Invalid username: must contain at least one non-numeric character.")
						continue
					}
					password = bytes.ReplaceAll(password, []byte(" "), []byte("_"))
					a := &account{
						email:    email,
						username: username,
						password: password,
					}
					err := registerAccount(s.passwordSalt, a)
					if err != nil {
						cmd.client.Terminate(fmt.Sprintf("Failed to register: %s", err))
						continue
					}
				} else {
					s.clientsLock.Lock()

					readUsername := func() bool {
						if cmd.client.json {
							if len(params) > 1 {
								username = params[1]
							}
						} else {
							if len(params) > 0 {
								username = params[0]
							}
						}
						if len(bytes.TrimSpace(username)) == 0 {
							username = s.randomUsername()
							randomUsername = true
						} else if !alphaNumericUnderscore.Match(username) {
							cmd.client.Terminate("Invalid username: must contain only letters, numbers and underscores.")
							return false
						}
						if onlyNumbers.Match(username) {
							cmd.client.Terminate("Invalid username: must contain at least one non-numeric character.")
							return false
						} else if s.clientByUsername(username) != nil || s.clientByUsername(append([]byte("Guest_"), username...)) != nil || (!randomUsername && !s.nameAllowed(username)) {
							cmd.client.Terminate("That username is already in use.")
							return false
						}
						return true
					}
					if !readUsername() {
						s.clientsLock.Unlock()
						continue
					}
					if len(params) > 2 {
						password = bytes.ReplaceAll(bytes.Join(params[2:], []byte(" ")), []byte(" "), []byte("_"))
					}

					s.clientsLock.Unlock()
				}

				if len(password) > 0 {
					a, err := loginAccount(s.passwordSalt, username, password)
					if err != nil {
						cmd.client.Terminate(fmt.Sprintf("Failed to log in: %s", err))
						continue
					} else if a == nil {
						cmd.client.Terminate("No account was found with the provided username and password. To log in as a guest, do not enter a password.")
						continue
					}

					var name []byte
					if bytes.HasPrefix(a.username, []byte("bot_")) {
						name = append([]byte("BOT_"), a.username[4:]...)
					} else {
						name = a.username
					}
					if s.clientByUsername(name) != nil {
						cmd.client.Terminate("That username is already in use.")
						continue
					}

					cmd.client.account = a
					cmd.client.accountID = a.id
					cmd.client.name = name
					cmd.client.autoplay = a.autoplay
				} else {
					cmd.client.accountID = 0
					if !randomUsername && !bytes.HasPrefix(username, []byte("BOT_")) && !bytes.HasPrefix(username, []byte("Guest_")) {
						username = append([]byte("Guest_"), username...)
					}
					cmd.client.name = username
				}

				cmd.client.sendEvent(&bgammon.EventWelcome{
					PlayerName: string(cmd.client.name),
					Clients:    len(s.clients),
					Games:      len(s.games),
				})

				log.Printf("Client %d logged in as %s", cmd.client.id, cmd.client.name)

				// Send user settings.
				if cmd.client.account != nil {
					a := cmd.client.account
					cmd.client.sendEvent(&bgammon.EventSettings{
						AutoPlay:  a.autoplay,
						Highlight: a.highlight,
						Pips:      a.pips,
						Moves:     a.moves,
						Flip:      a.flip,
						Advanced:  a.advanced,
						Speed:     a.speed,
					})
				}

				// Send message of the day.
				cmd.client.sendNotice("Connect with other players and stay up to date on the latest changes. Visit bgammon.org/community")

				// Rejoin match in progress.
				s.gamesLock.RLock()
				for _, g := range s.games {
					if g.terminated() || g.Winner != 0 {
						continue
					}

					var rejoin bool
					if bytes.Equal(cmd.client.name, g.allowed1) {
						rejoin = g.rejoin1
					} else if bytes.Equal(cmd.client.name, g.allowed2) {
						rejoin = g.rejoin2
					}
					if rejoin {
						g.addClient(cmd.client)
						cmd.client.sendNotice(fmt.Sprintf("Rejoined match: %s", g.name))
					}
				}
				s.gamesLock.RUnlock()
				continue
			}

			cmd.client.Terminate("You must login before using other commands.")
			continue
		}

		clientGame := s.gameByClient(cmd.client)
		if clientGame != nil && clientGame.client1 != cmd.client && clientGame.client2 != cmd.client {
			switch keyword {
			case bgammon.CommandHelp, "h", bgammon.CommandJSON, bgammon.CommandList, "ls", bgammon.CommandBoard, "b", bgammon.CommandLeave, "l", bgammon.CommandReplay, bgammon.CommandSet, bgammon.CommandDisconnect, bgammon.CommandPong:
				// These commands are allowed to be used by spectators.
			default:
				cmd.client.sendNotice("Command ignored: You are spectating this match.")
				continue
			}
		}

		switch keyword {
		case bgammon.CommandHelp, "h":
			// TODO get extended help by specifying a command after help
			cmd.client.sendEvent(&bgammon.EventHelp{
				Topic:   "",
				Message: "Test help text",
			})
		case bgammon.CommandJSON:
			sendUsage := func() {
				cmd.client.sendNotice("To enable JSON formatted messages, send 'json on'. To disable JSON formatted messages, send 'json off'.")
			}
			if len(params) != 1 {
				sendUsage()
				continue
			}
			paramLower := strings.ToLower(string(params[0]))
			switch paramLower {
			case "on":
				cmd.client.json = true
				cmd.client.sendNotice("JSON formatted messages enabled.")
			case "off":
				cmd.client.json = false
				cmd.client.sendNotice("JSON formatted messages disabled.")
			default:
				sendUsage()
			}
		case bgammon.CommandSay, "s":
			if len(params) == 0 {
				continue
			}
			if clientGame == nil {
				cmd.client.sendNotice("Message not sent: You are not currently in a match.")
				continue
			}
			opponent := clientGame.opponent(cmd.client)
			if opponent == nil {
				cmd.client.sendNotice("Message not sent: There is no one else in the match.")
				continue
			}
			ev := &bgammon.EventSay{
				Message: string(bytes.Join(params, []byte(" "))),
			}
			ev.Player = string(cmd.client.name)
			opponent.sendEvent(ev)
			if s.relayChat {
				for _, spectator := range clientGame.spectators {
					spectator.sendEvent(ev)
				}
			}
		case bgammon.CommandList, "ls":
			ev := &bgammon.EventList{}

			s.gamesLock.RLock()
			for _, g := range s.games {
				listing := g.listing(cmd.client.name)
				if listing == nil {
					continue
				}
				ev.Games = append(ev.Games, *listing)
			}
			s.gamesLock.RUnlock()

			cmd.client.sendEvent(ev)
		case bgammon.CommandCreate, "c":
			if clientGame != nil {
				cmd.client.sendNotice("Failed to create match: Please leave the match you are in before creating another.")
				continue
			}

			sendUsage := func() {
				cmd.client.sendNotice("To create a public match please specify whether it is public or private, and also specify how many points are needed to win the match. When creating a private match, a password must also be provided.")
			}
			if len(params) < 2 {
				sendUsage()
				continue
			}

			var gamePassword []byte
			gameType := bytes.ToLower(params[0])
			var gameName []byte
			var gamePoints []byte
			switch {
			case bytes.Equal(gameType, []byte("public")):
				gamePoints = params[1]
				if len(params) > 2 {
					gameName = bytes.Join(params[2:], []byte(" "))
				}
			case bytes.Equal(gameType, []byte("private")):
				if len(params) < 3 {
					sendUsage()
					continue
				}
				gamePassword = bytes.ReplaceAll(params[1], []byte("_"), []byte(" "))
				gamePoints = params[2]
				if len(params) > 3 {
					gameName = bytes.Join(params[3:], []byte(" "))
				}
			default:
				sendUsage()
				continue
			}

			variant := bgammon.VariantBackgammon

			// Backwards-compatible acey-deucey and tabula parameter. Acey-deucey added in v1.1.5. Tabula added in v1.2.2.
			variantNone := bytes.HasPrefix(gameName, []byte("0 ")) || bytes.Equal(gameName, []byte("0"))
			variantAcey := bytes.HasPrefix(gameName, []byte("1 ")) || bytes.Equal(gameName, []byte("1"))
			variantTabula := bytes.HasPrefix(gameName, []byte("2 ")) || bytes.Equal(gameName, []byte("2"))
			if variantNone || variantAcey || variantTabula {
				if variantAcey {
					variant = bgammon.VariantAceyDeucey
				} else if variantTabula {
					variant = bgammon.VariantTabula
				}
				if len(gameName) > 1 {
					gameName = gameName[2:]
				} else {
					gameName = nil
				}
			}

			points, err := strconv.Atoi(string(gamePoints))
			if err != nil || points < 1 || points > 99 {
				sendUsage()
				continue
			}

			// Set default game name.
			if len(bytes.TrimSpace(gameName)) == 0 {
				abbr := "'s"
				lastLetter := cmd.client.name[len(cmd.client.name)-1]
				if lastLetter == 's' || lastLetter == 'S' {
					abbr = "'"
				}
				gameName = []byte(fmt.Sprintf("%s%s match", cmd.client.name, abbr))
			}

			g := newServerGame(<-s.newGameIDs, variant)
			g.name = gameName
			g.Points = int8(points)
			g.password = gamePassword
			g.addClient(cmd.client)

			s.gamesLock.Lock()
			s.games = append(s.games, g)
			s.gamesLock.Unlock()

			cmd.client.sendNotice(fmt.Sprintf("Created match: %s", g.name))

			if len(g.password) == 0 {
				cmd.client.sendNotice("Note: Please be patient as you wait for another player to join the match. A chime will sound when another player joins. While you wait, join the bgammon.org community via Discord, Matrix or IRC at bgammon.org/community")
			}
		case bgammon.CommandJoin, "j":
			if clientGame != nil {
				cmd.client.sendEvent(&bgammon.EventFailedJoin{
					Reason: "Please leave the match you are in before joining another.",
				})
				continue
			}

			sendUsage := func() {
				cmd.client.sendNotice("To join a match please specify its ID or the name of a player in the match. To join a private match, a password must also be specified.")
			}

			if len(params) == 0 {
				sendUsage()
				continue
			}

			var joinGameID int
			if onlyNumbers.Match(params[0]) {
				gameID, err := strconv.Atoi(string(params[0]))
				if err == nil && gameID > 0 {
					joinGameID = gameID
				}

				if joinGameID == 0 {
					sendUsage()
					continue
				}
			} else {
				paramLower := bytes.ToLower(params[0])
				s.clientsLock.Lock()
				for _, sc := range s.clients {
					if bytes.Equal(paramLower, bytes.ToLower(sc.name)) {
						g := s.gameByClient(sc)
						if g != nil {
							joinGameID = g.id
						}
						break
					}
				}
				s.clientsLock.Unlock()

				if joinGameID == 0 {
					cmd.client.sendEvent(&bgammon.EventFailedJoin{
						Reason: "Match not found.",
					})
					continue
				}
			}

			s.gamesLock.Lock()
			for _, g := range s.games {
				if g.terminated() {
					continue
				}
				if g.id == joinGameID {
					providedPassword := bytes.ReplaceAll(bytes.Join(params[1:], []byte(" ")), []byte("_"), []byte(" "))
					if len(g.password) != 0 && (len(params) < 2 || !bytes.Equal(g.password, providedPassword)) {
						cmd.client.sendEvent(&bgammon.EventFailedJoin{
							Reason: "Invalid password.",
						})
						s.gamesLock.Unlock()
						continue COMMANDS
					}

					if bytes.HasPrefix(bytes.ToLower(cmd.client.name), []byte("bot_")) && ((g.client1 != nil && !bytes.HasPrefix(bytes.ToLower(g.client1.name), []byte("bot_"))) || (g.client2 != nil && !bytes.HasPrefix(bytes.ToLower(g.client2.name), []byte("bot_")))) {
						cmd.client.sendEvent(&bgammon.EventFailedJoin{
							Reason: "Bots are not allowed to join player matches. Please create a match instead.",
						})
						continue COMMANDS
					}

					spectator := g.addClient(cmd.client)
					s.gamesLock.Unlock()
					cmd.client.sendNotice(fmt.Sprintf("Joined match: %s", g.name))
					if spectator {
						cmd.client.sendNotice("You are spectating this match. Chat messages are not relayed.")
					}
					continue COMMANDS
				}
			}
			s.gamesLock.Unlock()

			cmd.client.sendEvent(&bgammon.EventFailedJoin{
				Reason: "Match not found.",
			})
		case bgammon.CommandLeave, "l":
			if clientGame == nil {
				cmd.client.sendEvent(&bgammon.EventFailedLeave{
					Reason: "You are not currently in a match.",
				})
				continue
			}

			if cmd.client.playerNumber == 1 {
				clientGame.rejoin1 = false
			} else {
				clientGame.rejoin2 = false
			}

			clientGame.removeClient(cmd.client)
		case bgammon.CommandDouble, "d":
			if clientGame == nil {
				cmd.client.sendNotice("You are not currently in a match.")
				continue
			} else if clientGame.Winner != 0 {
				continue
			}

			if clientGame.Turn != cmd.client.playerNumber {
				cmd.client.sendNotice("It is not your turn.")
				continue
			}

			gameState := &bgammon.GameState{
				Game:         clientGame.Game,
				PlayerNumber: cmd.client.playerNumber,
				Available:    clientGame.LegalMoves(false),
			}
			if !gameState.MayDouble() {
				cmd.client.sendNotice("You may not double at this time.")
				continue
			}

			if clientGame.DoublePlayer != 0 && clientGame.DoublePlayer != cmd.client.playerNumber {
				cmd.client.sendNotice("You do not currently hold the doubling cube.")
				continue
			}

			opponent := clientGame.opponent(cmd.client)
			if opponent == nil {
				cmd.client.sendNotice("You may not double until your opponent rejoins the match.")
				continue
			}

			clientGame.DoubleOffered = true
			clientGame.NextPartialTurn(opponent.playerNumber)

			cmd.client.sendNotice(fmt.Sprintf("Double offered to opponent (%d points).", clientGame.DoubleValue*2))
			clientGame.opponent(cmd.client).sendNotice(fmt.Sprintf("%s offers a double (%d points).", cmd.client.name, clientGame.DoubleValue*2))

			clientGame.eachClient(func(client *serverClient) {
				if client.json {
					clientGame.sendBoard(client, false)
				}
			})
		case bgammon.CommandResign:
			if clientGame == nil {
				cmd.client.sendNotice("You are not currently in a match.")
				continue
			} else if clientGame.Winner != 0 {
				continue
			}

			gameState := &bgammon.GameState{
				Game:         clientGame.Game,
				PlayerNumber: cmd.client.playerNumber,
				Available:    clientGame.LegalMoves(false),
			}
			if !gameState.MayResign() {
				cmd.client.sendNotice("You may not resign at this time.")
				continue
			}

			opponent := clientGame.opponent(cmd.client)
			if opponent == nil {
				cmd.client.sendNotice("You may not resign until your opponent rejoins the match.")
				continue
			}

			clientGame.NextPartialTurn(opponent.playerNumber)

			cmd.client.sendNotice("Declined double offer")
			clientGame.opponent(cmd.client).sendNotice(fmt.Sprintf("%s declined double offer.", cmd.client.name))

			clientGame.replay = append([][]byte{[]byte(fmt.Sprintf("i %d %s %s %d %d %d %d %d %d", clientGame.Started.Unix(), clientGame.Player1.Name, clientGame.Player2.Name, clientGame.Points, clientGame.Player1.Points, clientGame.Player2.Points, clientGame.Winner, clientGame.DoubleValue, clientGame.Variant))}, clientGame.replay...)

			clientGame.replay = append(clientGame.replay, []byte(fmt.Sprintf("%d d %d 0", clientGame.Turn, clientGame.DoubleValue*2)))

			var reset bool
			if cmd.client.playerNumber == 1 {
				clientGame.Player2.Points = clientGame.Player2.Points + clientGame.DoubleValue
				if clientGame.Player2.Points >= clientGame.Points {
					clientGame.Winner = 2
					clientGame.Ended = time.Now()
				} else {
					reset = true
				}
			} else {
				clientGame.Player1.Points = clientGame.Player2.Points + clientGame.DoubleValue
				if clientGame.Player1.Points >= clientGame.Points {
					clientGame.Winner = 1
					clientGame.Ended = time.Now()
				} else {
					reset = true
				}
			}

			var winEvent *bgammon.EventWin
			if clientGame.Winner != 0 {
				winEvent = &bgammon.EventWin{
					Points: clientGame.DoubleValue,
				}
				if clientGame.Winner == 1 {
					winEvent.Player = clientGame.Player1.Name
				} else {
					winEvent.Player = clientGame.Player2.Name
				}

				err := recordGameResult(clientGame, 4, clientGame.replay)
				if err != nil {
					log.Fatalf("failed to record game result: %s", err)
				}

				if !reset {
					err := recordMatchResult(clientGame, matchTypeCasual)
					if err != nil {
						log.Fatalf("failed to record match result: %s", err)
					}
				}
			}

			if reset {
				clientGame.Reset()
				clientGame.replay = clientGame.replay[:0]
			}

			clientGame.eachClient(func(client *serverClient) {
				clientGame.sendBoard(client, false)
				if winEvent != nil {
					client.sendEvent(winEvent)
				}
			})
		case bgammon.CommandRoll, "r":
			if clientGame == nil {
				cmd.client.sendEvent(&bgammon.EventFailedRoll{
					Reason: "You are not currently in a match.",
				})
				continue
			} else if clientGame.Winner != 0 {
				continue
			}

			opponent := clientGame.opponent(cmd.client)
			if opponent == nil {
				cmd.client.sendEvent(&bgammon.EventFailedRoll{
					Reason: "You may not roll until your opponent rejoins the match.",
				})
				continue
			}

			if !clientGame.roll(cmd.client.playerNumber) {
				cmd.client.sendEvent(&bgammon.EventFailedRoll{
					Reason: "It is not your turn to roll.",
				})
				continue
			}

			clientGame.eachClient(func(client *serverClient) {
				ev := &bgammon.EventRolled{
					Roll1: clientGame.Roll1,
					Roll2: clientGame.Roll2,
					Roll3: clientGame.Roll3,
				}
				ev.Player = string(cmd.client.name)
				if clientGame.Turn == 0 && client.playerNumber == 2 {
					ev.Roll1, ev.Roll2 = ev.Roll2, ev.Roll1
				}
				client.sendEvent(ev)
			})

			// Re-roll automatically when players roll the same value when starting a game.
			if clientGame.Turn == 0 && clientGame.Roll1 != 0 && clientGame.Roll2 != 0 {
				reroll := func() {
					clientGame.Roll1 = 0
					clientGame.Roll2 = 0
					if !clientGame.roll(clientGame.Turn) {
						log.Fatal("failed to re-roll while starting game")
					}

					ev := &bgammon.EventRolled{
						Roll1: clientGame.Roll1,
						Roll2: clientGame.Roll2,
						Roll3: clientGame.Roll3,
					}
					ev.Player = string(clientGame.Player1.Name)
					if clientGame.Turn == 2 {
						ev.Player = string(clientGame.Player2.Name)
					}
					clientGame.eachClient(func(client *serverClient) {
						client.sendEvent(ev)
					})
				}

				if clientGame.Roll1 > clientGame.Roll2 {
					clientGame.Turn = 1
					if clientGame.Variant != bgammon.VariantBackgammon {
						reroll()
					}
				} else if clientGame.Roll2 > clientGame.Roll1 {
					clientGame.Turn = 2
					if clientGame.Variant != bgammon.VariantBackgammon {
						reroll()
					}
				} else {
					for {
						clientGame.Roll1 = 0
						clientGame.Roll2 = 0
						if !clientGame.roll(1) {
							log.Fatal("failed to re-roll to determine starting player")
						}
						if !clientGame.roll(2) {
							log.Fatal("failed to re-roll to determine starting player")
						}
						clientGame.eachClient(func(client *serverClient) {
							{
								ev := &bgammon.EventRolled{
									Roll1: clientGame.Roll1,
								}
								ev.Player = clientGame.Player1.Name
								if clientGame.Turn == 0 && client.playerNumber == 2 {
									ev.Roll1, ev.Roll2 = ev.Roll2, ev.Roll1
								}
								client.sendEvent(ev)
							}
							{
								ev := &bgammon.EventRolled{
									Roll1: clientGame.Roll1,
									Roll2: clientGame.Roll2,
								}
								ev.Player = clientGame.Player2.Name
								if clientGame.Turn == 0 && client.playerNumber == 2 {
									ev.Roll1, ev.Roll2 = ev.Roll2, ev.Roll1
								}
								client.sendEvent(ev)
							}
						})
						if clientGame.Roll1 > clientGame.Roll2 {
							clientGame.Turn = 1
							if clientGame.Variant != bgammon.VariantBackgammon {
								reroll()
							}
							break
						} else if clientGame.Roll2 > clientGame.Roll1 {
							clientGame.Turn = 2
							if clientGame.Variant != bgammon.VariantBackgammon {
								reroll()
							}
							break
						}
					}
				}
			}

			clientGame.NextPartialTurn(clientGame.Turn)

			forcedMove := clientGame.playForcedMoves()
			if forcedMove && len(clientGame.LegalMoves(false)) == 0 {
				chooseRoll := clientGame.Variant == bgammon.VariantAceyDeucey && ((clientGame.Roll1 == 1 && clientGame.Roll2 == 2) || (clientGame.Roll1 == 2 && clientGame.Roll2 == 1)) && len(clientGame.Moves) == 2
				if clientGame.Variant != bgammon.VariantAceyDeucey || !chooseRoll {
					clientGame.recordEvent()
					clientGame.nextTurn(false)
					continue
				}
			}

			clientGame.eachClient(func(client *serverClient) {
				if clientGame.Turn != 0 || !client.json {
					clientGame.sendBoard(client, false)
				}
			})
		case bgammon.CommandMove, "m", "mv":
			if clientGame == nil {
				cmd.client.sendEvent(&bgammon.EventFailedMove{
					Reason: "You are not currently in a match.",
				})
				continue
			} else if clientGame.Winner != 0 {
				clientGame.sendBoard(cmd.client, false)
				continue
			}

			if clientGame.Turn != cmd.client.playerNumber {
				cmd.client.sendEvent(&bgammon.EventFailedMove{
					Reason: "It is not your turn to move.",
				})
				continue
			}

			opponent := clientGame.opponent(cmd.client)
			if opponent == nil {
				cmd.client.sendEvent(&bgammon.EventFailedMove{
					Reason: "You may not move until your opponent rejoins the match.",
				})
				continue
			}

			sendUsage := func() {
				cmd.client.sendEvent(&bgammon.EventFailedMove{
					Reason: "Specify one or more moves in the form FROM/TO. For example: 8/4 6/4",
				})
			}

			if len(params) == 0 {
				sendUsage()
				continue
			}

			var moves [][]int8
			for i := range params {
				split := bytes.Split(params[i], []byte("/"))
				if len(split) != 2 {
					sendUsage()
					continue COMMANDS
				}
				from := bgammon.ParseSpace(string(split[0]))
				if from == -1 {
					sendUsage()
					continue COMMANDS
				}
				to := bgammon.ParseSpace(string(split[1]))
				if to == -1 {
					sendUsage()
					continue COMMANDS
				}
				if !bgammon.ValidSpace(from) || !bgammon.ValidSpace(to) {
					cmd.client.sendEvent(&bgammon.EventFailedMove{
						From:   from,
						To:     to,
						Reason: "Illegal move.",
					})
					continue COMMANDS
				}

				from, to = bgammon.FlipSpace(from, cmd.client.playerNumber, clientGame.Variant), bgammon.FlipSpace(to, cmd.client.playerNumber, clientGame.Variant)
				moves = append(moves, []int8{from, to})
			}

			ok, expandedMoves := clientGame.AddMoves(moves, false)
			if !ok {
				cmd.client.sendEvent(&bgammon.EventFailedMove{
					From:   0,
					To:     0,
					Reason: "Illegal move.",
				})
				continue
			}

			clientGame.eachClient(func(client *serverClient) {
				ev := &bgammon.EventMoved{
					Moves: bgammon.FlipMoves(expandedMoves, client.playerNumber, clientGame.Variant),
				}
				ev.Player = string(cmd.client.name)
				client.sendEvent(ev)

				clientGame.sendBoard(client, false)
			})

			clientGame.handleWin()
		case bgammon.CommandReset:
			if clientGame == nil {
				cmd.client.sendNotice("You are not currently in a match.")
				continue
			} else if clientGame.Winner != 0 {
				continue
			}

			if clientGame.Turn != cmd.client.playerNumber {
				cmd.client.sendNotice("It is not your turn.")
				continue
			}

			if len(clientGame.Moves) == 0 {
				continue
			}

			l := len(clientGame.Moves)
			undoMoves := make([][]int8, l)
			for i, move := range clientGame.Moves {
				undoMoves[l-1-i] = []int8{move[1], move[0]}
			}
			ok, _ := clientGame.AddMoves(undoMoves, false)
			if !ok {
				cmd.client.sendNotice("Failed to undo move: invalid move.")
			} else {
				clientGame.eachClient(func(client *serverClient) {
					ev := &bgammon.EventMoved{
						Moves: bgammon.FlipMoves(undoMoves, client.playerNumber, clientGame.Variant),
					}
					ev.Player = string(cmd.client.name)

					client.sendEvent(ev)
					clientGame.sendBoard(client, false)
				})
			}
		case bgammon.CommandOk, "k":
			if clientGame == nil {
				cmd.client.sendNotice("You are not currently in a match.")
				continue
			} else if clientGame.Winner != 0 {
				continue
			}

			opponent := clientGame.opponent(cmd.client)
			if opponent == nil {
				cmd.client.sendNotice("You must wait until your opponent rejoins the match before continuing the game.")
				continue
			}

			if clientGame.DoubleOffered {
				if clientGame.Turn != cmd.client.playerNumber {
					opponent := clientGame.opponent(cmd.client)
					if opponent == nil {
						cmd.client.sendNotice("You may not accept the double until your opponent rejoins the match.")
						continue
					}

					clientGame.DoubleOffered = false
					clientGame.DoubleValue = clientGame.DoubleValue * 2
					clientGame.DoublePlayer = cmd.client.playerNumber
					clientGame.NextPartialTurn(opponent.playerNumber)

					cmd.client.sendNotice("Accepted double.")
					opponent.sendNotice(fmt.Sprintf("%s accepted double.", cmd.client.name))

					clientGame.replay = append(clientGame.replay, []byte(fmt.Sprintf("%d d %d 1", clientGame.Turn, clientGame.DoubleValue)))
					clientGame.eachClient(func(client *serverClient) {
						clientGame.sendBoard(client, false)
					})
				} else {
					cmd.client.sendNotice("Waiting for response from opponent.")
				}
				continue
			} else if clientGame.Turn != cmd.client.playerNumber {
				cmd.client.sendNotice("It is not your turn.")
				continue
			}

			if clientGame.Roll1 == 0 || clientGame.Roll2 == 0 {
				cmd.client.sendNotice("You must roll first.")
				continue
			}

			legalMoves := clientGame.LegalMoves(false)
			if len(legalMoves) != 0 {
				available := bgammon.FlipMoves(legalMoves, cmd.client.playerNumber, clientGame.Variant)
				bgammon.SortMoves(available)
				cmd.client.sendEvent(&bgammon.EventFailedOk{
					Reason: fmt.Sprintf("The following legal moves are available: %s", bgammon.FormatMoves(available)),
				})
				continue
			}

			if clientGame.Variant == bgammon.VariantAceyDeucey && ((clientGame.Roll1 == 1 && clientGame.Roll2 == 2) || (clientGame.Roll1 == 2 && clientGame.Roll2 == 1)) && len(clientGame.Moves) == 2 {
				var doubles int
				if len(params) > 0 {
					doubles, _ = strconv.Atoi(string(params[0]))
				}
				if doubles < 1 || doubles > 6 {
					cmd.client.sendEvent(&bgammon.EventFailedOk{
						Reason: "Choose which doubles you want for your acey-deucey.",
					})
					continue
				}

				clientGame.recordEvent()
				clientGame.nextTurn(true)
				clientGame.Roll1, clientGame.Roll2 = int8(doubles), int8(doubles)
				clientGame.Reroll = true

				clientGame.eachClient(func(client *serverClient) {
					ev := &bgammon.EventRolled{
						Roll1:    clientGame.Roll1,
						Roll2:    clientGame.Roll2,
						Selected: true,
					}
					ev.Player = string(cmd.client.name)
					client.sendEvent(ev)
					clientGame.sendBoard(client, false)
				})
			} else if clientGame.Variant == bgammon.VariantAceyDeucey && clientGame.Reroll {
				clientGame.recordEvent()
				clientGame.nextTurn(true)
				clientGame.Roll1, clientGame.Roll2 = 0, 0
				if !clientGame.roll(cmd.client.playerNumber) {
					cmd.client.Terminate("Server error")
					opponent.Terminate("Server error")
					continue
				}
				clientGame.Reroll = false

				clientGame.eachClient(func(client *serverClient) {
					ev := &bgammon.EventRolled{
						Roll1: clientGame.Roll1,
						Roll2: clientGame.Roll2,
					}
					ev.Player = string(cmd.client.name)
					client.sendEvent(ev)
					clientGame.sendBoard(client, false)
				})
			} else {
				clientGame.recordEvent()
				clientGame.nextTurn(false)
			}
		case bgammon.CommandRematch, "rm":
			if clientGame == nil {
				cmd.client.sendNotice("You are not currently in a match.")
				continue
			} else if clientGame.Winner == 0 {
				cmd.client.sendNotice("The match you are in is still in progress.")
				continue
			} else if clientGame.rematch == cmd.client.playerNumber {
				cmd.client.sendNotice("You have already requested a rematch.")
				continue
			} else if clientGame.client1 == nil || clientGame.client2 == nil {
				cmd.client.sendNotice("Your opponent left the match.")
				continue
			} else if clientGame.rematch != 0 && clientGame.rematch != cmd.client.playerNumber {
				s.gamesLock.Lock()

				newGame := newServerGame(<-s.newGameIDs, clientGame.Variant)
				newGame.name = clientGame.name
				newGame.Points = clientGame.Points
				newGame.password = clientGame.password
				newGame.client1 = clientGame.client1
				newGame.client2 = clientGame.client2
				newGame.spectators = make([]*serverClient, len(clientGame.spectators))
				copy(newGame.spectators, clientGame.spectators)
				newGame.Player1.Name = clientGame.Player1.Name
				newGame.Player2.Name = clientGame.Player2.Name
				newGame.Player1.Points = clientGame.Player1.Points
				newGame.Player2.Points = clientGame.Player2.Points
				newGame.allowed1 = clientGame.allowed1
				newGame.allowed2 = clientGame.allowed2
				s.games = append(s.games, newGame)

				clientGame.client1 = nil
				clientGame.client2 = nil
				clientGame.spectators = nil

				s.gamesLock.Unlock()

				{
					ev1 := &bgammon.EventJoined{
						GameID:       newGame.id,
						PlayerNumber: 1,
					}
					ev1.Player = newGame.Player1.Name
					ev2 := &bgammon.EventJoined{
						GameID:       newGame.id,
						PlayerNumber: 2,
					}
					ev2.Player = newGame.Player2.Name
					newGame.client1.sendEvent(ev1)
					newGame.client1.sendEvent(ev2)
					newGame.sendBoard(newGame.client1, false)
				}

				{
					ev1 := &bgammon.EventJoined{
						GameID:       newGame.id,
						PlayerNumber: 1,
					}
					ev1.Player = newGame.Player2.Name
					ev2 := &bgammon.EventJoined{
						GameID:       newGame.id,
						PlayerNumber: 2,
					}
					ev2.Player = newGame.Player1.Name
					newGame.client2.sendEvent(ev1)
					newGame.client2.sendEvent(ev2)
					newGame.sendBoard(newGame.client2, false)
				}

				for _, spectator := range newGame.spectators {
					newGame.sendBoard(spectator, false)
				}
			} else {
				clientGame.rematch = cmd.client.playerNumber

				clientGame.opponent(cmd.client).sendNotice("Your opponent would like to play again. Type /rematch to accept.")
				cmd.client.sendNotice("Rematch offer sent.")
				continue
			}
		case bgammon.CommandBoard, "b":
			if clientGame == nil {
				cmd.client.sendNotice("You are not currently in a match.")
				continue
			}

			clientGame.sendBoard(cmd.client, false)
		case bgammon.CommandPassword:
			if cmd.client.account == nil {
				cmd.client.sendNotice("Failed to change password: you are logged in as a guest.")
				continue
			} else if len(params) < 2 {
				cmd.client.sendNotice("Please specify your old and new passwords as follows: password <old> <new>")
				continue
			}

			a, err := loginAccount(s.passwordSalt, cmd.client.name, params[0])
			if err != nil || a == nil || a.id == 0 {
				cmd.client.sendNotice("Failed to change password: incorrect existing password.")
				continue
			}

			err = setAccountPassword(s.passwordSalt, a.id, string(bytes.Join(params[1:], []byte("_"))))
			if err != nil {
				cmd.client.sendNotice("Failed to change password.")
				continue
			}
			cmd.client.sendNotice("Password changed successfully.")
		case bgammon.CommandSet:
			if len(params) < 2 {
				cmd.client.sendNotice("Please specify the setting name and value as follows: set <name> <value>")
				continue
			}

			name := string(bytes.ToLower(params[0]))
			settings := []string{"autoplay", "highlight", "pips", "moves", "flip", "advanced", "speed"}
			var found bool
			for i := range settings {
				if name == settings[i] {
					found = true
					break
				}
			}
			if !found {
				cmd.client.sendNotice("Please specify the setting name and value as follows: set <name> <value>")
				continue
			}

			value, err := strconv.Atoi(string(params[1]))
			if err != nil || value < 0 || (name == "speed" && value > 3) || (name != "speed" && value > 1) {
				cmd.client.sendNotice("Invalid setting value provided.")
				continue
			}

			if name == "autoplay" {
				cmd.client.autoplay = value == 1
			}

			if cmd.client.account == nil {
				continue
			}
			_ = setAccountSetting(cmd.client.account.id, name, value)
		case bgammon.CommandReplay:
			var (
				id     int
				replay []byte
				err    error
			)
			if len(params) == 0 {
				if clientGame == nil || clientGame.Winner == 0 {
					cmd.client.sendNotice("Please specify the game as follows: replay <id>")
					continue
				}
				id = -1
				replay = bytes.Join(clientGame.replay, []byte("\n"))
			} else {
				id, err = strconv.Atoi(string(params[0]))
				if err != nil || id < 0 {
					cmd.client.sendNotice("Invalid replay ID provided.")
					continue
				}
				replay, err = replayByID(id)
				if err != nil {
					cmd.client.sendNotice("Invalid replay ID provided.")
					continue
				}
			}
			if len(replay) == 0 {
				cmd.client.sendNotice("No replay was recorded for that game.")
				continue
			}
			cmd.client.sendEvent(&bgammon.EventReplay{
				ID:      id,
				Content: replay,
			})
		case bgammon.CommandHistory:
			if len(params) == 0 {
				cmd.client.sendNotice("Please specify the player as follows: history <username>")
				continue
			}
			const historyPageSize = 50

			page := 1
			if len(params) > 1 {
				p, err := strconv.Atoi(string(params[1]))
				if err == nil && p >= 1 {
					page = p
				}
			}

			matches, err := matchHistory(string(params[0]))
			if err != nil {
				cmd.client.sendNotice("Invalid replay ID provided.")
				continue
			}

			pages := (len(matches) / historyPageSize)
			if pages == 0 {
				pages = 1
			}

			ev := &bgammon.EventHistory{
				Page:  page,
				Pages: pages,
			}
			if len(matches) > 0 && page <= pages {
				max := page * historyPageSize
				if max > len(matches) {
					max = len(matches)
				}
				ev.Matches = matches[(page-1)*historyPageSize : max]
			}

			ev.Player = string(params[0])
			a, err := accountByUsername(string(params[0]))
			if err == nil && a != nil {
				ev.CasualBackgammonSingle = a.casual.backgammonSingle / 100
				ev.CasualBackgammonMulti = a.casual.backgammonMulti / 100
				ev.CasualAceyDeuceySingle = a.casual.aceySingle / 100
				ev.CasualAceyDeuceyMulti = a.casual.aceyMulti / 100
				ev.CasualTabulaSingle = a.casual.tabulaSingle / 100
				ev.CasualTabulaMulti = a.casual.tabulaMulti / 100
			}
			cmd.client.sendEvent(ev)
		case bgammon.CommandDisconnect:
			if clientGame != nil {
				clientGame.removeClient(cmd.client)
			}
			cmd.client.Terminate("Client disconnected")
		case bgammon.CommandPong:
			// Do nothing.
		case "endgame":
			if !allowDebugCommands {
				cmd.client.sendNotice("You are not allowed to use that command.")
				continue
			}

			if clientGame == nil {
				cmd.client.sendNotice("You are not currently in a match.")
				continue
			}

			clientGame.Turn = 1
			clientGame.Roll1 = 6
			clientGame.Roll2 = 1
			clientGame.Roll3 = 1
			clientGame.Variant = 2
			clientGame.Player1.Entered = true
			clientGame.Player2.Entered = true
			clientGame.Board = []int8{0, 0, 0, 0, 0, -3, 0, 0, -3, -2, -2, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0, 9, 3, 1, -5, 1, 1, 0}

			log.Println(clientGame.Board[0:28])

			clientGame.eachClient(func(client *serverClient) {
				clientGame.sendBoard(client, false)
			})
		default:
			log.Printf("Received unknown command from client %s: %s", cmd.client.label(), cmd.command)
			cmd.client.sendNotice(fmt.Sprintf("Unknown command: %s", cmd.command))
		}
	}
}
