package server

import (
	"bytes"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"codeberg.org/tslocum/bgammon"
	"codeberg.org/tslocum/gotext"
)

var clearBytes = []byte("clear")

// translatedLanguages is a list of languages which have at least 90% translation status.
var translatedLanguages = []string{
	"en",
	"de",
	"es_mx",
	"fr",
	"uk",
}

// handleFirstCommand handles the login and register commands. This method runs
// in a separate goroutine to allow gameplay to continue while checking passwords.
func (s *server) handleFirstCommand(cmd serverCommand, keyword string, params [][]byte, reigster bool) {
	if keyword == bgammon.CommandLoginJSON || keyword == bgammon.CommandRegisterJSON || keyword == "lj" || keyword == "rj" {
		cmd.client.json = true
	}

	var username []byte
	var password []byte
	var randomUsername bool
	if reigster {
		sendUsage := func() {
			cmd.client.Terminate(gotext.GetD(cmd.client.language, "Please enter an email, username and password."))
		}

		if s.defcon == 1 {
			cmd.client.Terminate(gotext.GetD(cmd.client.language, "Due to ongoing abuse, registration is disabled. Please try again later."))
			return
		}

		var email []byte
		if keyword == bgammon.CommandRegisterJSON || keyword == "rj" {
			if len(params) < 4 {
				sendUsage()
				return
			}
			cmd.client.legacy = legacyClient(params[0])
			slashIndex := bytes.IndexRune(params[0], '/')
			if slashIndex != -1 {
				cmd.client.language = "bgammon-" + string(s.matchLanguage(params[0][slashIndex+1:]))
			}
			email = params[1]
			username = params[2]
			password = bytes.Join(params[3:], []byte("_"))
		} else {
			if len(params) < 3 {
				sendUsage()
				return
			}
			email = params[0]
			username = params[1]
			password = bytes.Join(params[2:], []byte("_"))
		}
		email = bytes.ToLower(email)
		username = bytes.ToLower(username)
		if onlyNumbers.Match(username) {
			cmd.client.Terminate(gotext.GetD(cmd.client.language, "Failed to register: Invalid username: must contain at least one non-numeric character."))
			return
		} else if len(string(username)) > maxUsernameLength {
			cmd.client.Terminate(fmt.Sprintf(gotext.GetD(cmd.client.language, "Failed to register: %s", gotext.GetD(cmd.client.language, "Invalid username: must be %d characters or less.")), maxUsernameLength))
			return
		}
		password = bytes.ReplaceAll(password, []byte(" "), []byte("_"))
		a := &account{
			email:    email,
			username: username,
			password: password,
		}
		err := registerAccount(s.passwordSalt, a, cmd.client.Address())
		if err != nil {
			cmd.client.Terminate(fmt.Sprintf("Failed to register: %s", err))
			return
		}
	} else {
		s.clientsLock.Lock()

		readUsername := func() bool {
			if cmd.client.json {
				if len(params) > 0 {
					cmd.client.legacy = legacyClient(params[0])
					slashIndex := bytes.IndexRune(params[0], '/')
					if slashIndex != -1 {
						cmd.client.language = "bgammon-" + string(s.matchLanguage(params[0][slashIndex+1:]))
					}
					if len(params) > 1 {
						username = params[1]
					}
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
				cmd.client.Terminate(gotext.GetD(cmd.client.language, "Invalid username: must contain only letters, numbers and underscores."))
				return false
			}
			username = bytes.ToLower(username)
			if onlyNumbers.Match(username) {
				cmd.client.Terminate(gotext.GetD(cmd.client.language, "Invalid username: must contain at least one non-numeric character."))
				return false
			} else if s.clientByUsername(username) != nil || s.clientByUsername(append([]byte("Guest_"), username...)) != nil || (!randomUsername && !s.nameAllowed(username)) {
				cmd.client.Terminate(gotext.GetD(cmd.client.language, "That username is already in use."))
				return false
			} else if (!bytes.HasPrefix(username, []byte("guest_")) && len(string(username)) > maxUsernameLength) || len(string(username)) > maxUsernameLength+6 {
				cmd.client.Terminate(fmt.Sprintf(gotext.GetD(cmd.client.language, "Invalid username: must be %d characters or less."), maxUsernameLength))
				return false
			}
			return true
		}
		if !readUsername() {
			s.clientsLock.Unlock()
			return
		}
		if len(params) > 2 {
			password = bytes.ReplaceAll(bytes.Join(params[2:], []byte(" ")), []byte(" "), []byte("_"))
		}

		s.clientsLock.Unlock()
	}

	if len(password) > 0 {
		a, err := loginAccount(s.passwordSalt, username, password)
		if err != nil {
			cmd.client.Terminate(fmt.Sprintf(gotext.GetD(cmd.client.language, "Failed to log in: %s"), err))
			return
		} else if a == nil {
			cmd.client.Terminate(gotext.GetD(cmd.client.language, "No account was found with the provided username and password. To log in as a guest, do not enter a password."))
			return
		}

		var name []byte
		if bytes.HasPrefix(a.username, []byte("bot_")) {
			name = append([]byte("BOT_"), a.username[4:]...)
		} else {
			name = a.username
		}
		s.clientsLock.Lock()
		existing := s.clientByUsername(name)
		s.clientsLock.Unlock()
		if existing != nil {
			cmd.client.Terminate(gotext.GetD(cmd.client.language, "That username is already in use."))
			return
		}

		cmd.client.account = a
		cmd.client.accountID = a.id
		cmd.client.name = name
		cmd.client.autoplay = a.autoplay
	} else {
		cmd.client.accountID = 0
		if bytes.HasPrefix(username, []byte("bot_")) {
			username = append([]byte("BOT_"), username[4:]...)
		} else if bytes.HasPrefix(username, []byte("guest_")) {
			username = append([]byte("Guest_"), username[6:]...)
		} else if !randomUsername {
			username = append([]byte("Guest_"), username...)
		}
		cmd.client.name = username
	}

	if s.defcon <= 2 {
		cmd.client.Terminate(gotext.GetD(cmd.client.language, "Due to ongoing abuse, only registered users may connect. Please register an account or try again later."))
		return
	}

	banned, banReason := checkBan(cmd.client.Address(), cmd.client.accountID)
	if banned {
		msg := "You are banned"
		if banReason != "" {
			msg += ": " + banReason
		}
		cmd.client.Terminate(msg)
		return
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
			AutoPlay:      a.autoplay,
			Highlight:     a.highlight,
			Pips:          a.pips,
			Moves:         a.moves,
			Flip:          a.flip,
			Traditional:   a.traditional,
			Advanced:      a.advanced,
			MuteJoinLeave: a.muteJoinLeave,
			MuteChat:      a.muteChat,
			MuteRoll:      a.muteRoll,
			MuteMove:      a.muteMove,
			MuteBearOff:   a.muteBearOff,
			Dim:           a.dim,
			Speed:         a.speed,
		})
	}

	// Send match list.
	s.sendMatchList(cmd.client)

	// Send message of the day.
	s.sendMOTD(cmd.client)

	// Request translation assistance from international users.
	if strings.HasPrefix(cmd.client.language, "bgammon-") {
		clientLanguage := cmd.client.language[8:]
		if clientLanguage != "" {
			var found bool
			for _, language := range translatedLanguages {
				if language == clientLanguage {
					found = true
					break
				}
			}
			if !found {
				cmd.client.sendNotice("Help translate this application into your preferred language at bgammon.org/translate")
			}
		}
	}

	// Send leacy client warning message.
	if cmd.client.legacy {
		cmd.client.sendNotice("Warning: You are using an outdated client. Please download the latest version at bgammon.org/download")
	}

	// Send DEFCON warning message.
	if s.defcon < 5 {
		cmd.client.sendDefconWarning(s.defcon)
	}

	// Send followed player notifications.
	c := cmd.client
	if c.accountID != 0 {
		s.clientsLock.Lock()
		for _, sc := range s.clients {
			if sc.accountID <= 0 {
				continue
			}
			for _, target := range c.account.follows {
				if sc.accountID == target {
					c.sendNotice(fmt.Sprintf(gotext.GetD(c.language, "%s is online."), sc.name))
				}
			}
			for _, target := range sc.account.follows {
				if c.accountID == target {
					sc.sendNotice(fmt.Sprintf(gotext.GetD(c.language, "%s is online."), c.name))
				}
			}
		}
		s.clientsLock.Unlock()
	}

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
			matchName := string(g.name)
			if g.Points > 1 {
				matchName = gotext.GetND(cmd.client.language, "%[1]s (%[2]d point)", "%[1]s (%[2]d points)", int(g.Points), g.name, g.Points)
			}
			cmd.client.sendNotice(gotext.GetD(cmd.client.language, "Rejoined match: %s", matchName))
		}
	}
	s.gamesLock.RUnlock()
}

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

		// Require users to login or register before using other commands.
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
				go s.handleFirstCommand(cmd, keyword, params, registerCommand)
				continue
			}

			cmd := cmd
			go func() {
				time.Sleep(500 * time.Millisecond)
				s.commands <- cmd
			}()
			continue
		}

		clientGame := s.gameByClient(cmd.client)
		if clientGame != nil && clientGame.client1 != cmd.client && clientGame.client2 != cmd.client {
			switch keyword {
			case bgammon.CommandHelp, "h", bgammon.CommandJSON, bgammon.CommandList, "ls", bgammon.CommandBoard, "b", bgammon.CommandLeave, "l", bgammon.CommandHistory, bgammon.CommandReplay, bgammon.CommandSet, bgammon.CommandPassword, bgammon.CommandFollow, bgammon.CommandUnfollow, bgammon.CommandPong, bgammon.CommandDisconnect, bgammon.CommandMOTD, bgammon.CommandBroadcast, bgammon.CommandDefcon, bgammon.CommandKick, bgammon.CommandBan, bgammon.CommandUnban, bgammon.CommandShutdown:
				// These commands are allowed to be used by spectators.
			default:
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "Command ignored: You are spectating this match."))
				continue
			}
		}

		switch keyword {
		case bgammon.CommandHelp, "h":
			if len(params) > 0 {
				command := string(bytes.ToLower(bytes.Join(params, []byte(" "))))
				commandHelp := bgammon.HelpText[command]
				if commandHelp != "" {
					cmd.client.sendNotice("/" + command + " " + commandHelp)
				} else {
					cmd.client.sendNotice(fmt.Sprintf("Unknown command: %s", command))
				}
				continue
			}

			cmd.client.sendNotice("Available commands:")
			for _, command := range s.sortedCommands {
				cmd.client.sendNotice("/" + command + " " + bgammon.HelpText[command])
			}
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
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "Message not sent: You are not currently in a match."))
				continue
			}
			opponent := clientGame.opponent(cmd.client)
			if opponent == nil {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "Message not sent: There is no one else in the match."))
				continue
			}
			if s.defcon <= 3 && cmd.client.accountID == 0 {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "Due to ongoing abuse, some actions are restricted to registered users only. Please log in or register to avoid interruptions."))
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
			s.sendMatchList(cmd.client)
		case bgammon.CommandCreate, "c":
			failCreate := func(message string) {
				cmd.client.sendEvent(&bgammon.EventFailedCreate{
					Reason: message,
				})
				// TODO Remove when most users are running Boxcars v1.4.7+
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "Failed to create match: %s", message))
			}
			sendUsage := func() {
				failCreate("To create a public match please specify whether it is public or private, and also specify how many points are needed to win the match. When creating a private match, a password must also be provided.")
			}

			if clientGame != nil {
				failCreate(gotext.GetD(cmd.client.language, "Please leave the match you are in before creating another."))
				continue
			} else if !s.shutdownTime.IsZero() {
				failCreate(gotext.GetD(cmd.client.language, "The server is shutting down. Reason: %s", s.shutdownReason))
				continue
			} else if len(params) < 2 {
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
			if err != nil || points < 1 {
				sendUsage()
				continue
			} else if points > 127 {
				points = 127
			}

			if s.defcon <= 3 && cmd.client.accountID == 0 {
				gameName = nil
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

			cmd.client.sendNotice(fmt.Sprintf(gotext.GetD(cmd.client.language, "Created match: %s"), g.name))

			if len(g.password) == 0 {
				cmd.client.sendNotice("Note: Please be patient as you wait for another player to join the match. A chime will sound when another player joins. While you wait, join the bgammon.org community via Discord, Matrix or IRC at bgammon.org/community")
			}
		case bgammon.CommandJoin, "j":
			if clientGame != nil {
				cmd.client.sendEvent(&bgammon.EventFailedJoin{
					Reason: gotext.GetD(cmd.client.language, "Please leave the match you are in before joining another."),
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
						Reason: gotext.GetD(cmd.client.language, "Match not found."),
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
							Reason: gotext.GetD(cmd.client.language, "Invalid password."),
						})
						s.gamesLock.Unlock()
						continue COMMANDS
					}

					if bytes.HasPrefix(bytes.ToLower(cmd.client.name), []byte("bot_")) && ((g.client1 != nil && !bytes.HasPrefix(bytes.ToLower(g.client1.name), []byte("bot_"))) || (g.client2 != nil && !bytes.HasPrefix(bytes.ToLower(g.client2.name), []byte("bot_")))) {
						cmd.client.sendEvent(&bgammon.EventFailedJoin{
							Reason: gotext.GetD(cmd.client.language, "Bots are not allowed to join player matches. Please create a match instead."),
						})
						continue COMMANDS
					}

					spectator := g.addClient(cmd.client)
					s.gamesLock.Unlock()
					matchName := string(g.name)
					if g.Points > 1 {
						matchName = gotext.GetND(cmd.client.language, "%[1]s (%[2]d point)", "%[1]s (%[2]d points)", int(g.Points), g.name, g.Points)
					}
					cmd.client.sendNotice(fmt.Sprintf(gotext.GetD(cmd.client.language, "Joined match: %s"), matchName))
					if spectator {
						cmd.client.sendNotice(gotext.GetD(cmd.client.language, "You are spectating this match. Chat messages are not relayed."))
					}
					continue COMMANDS
				}
			}
			s.gamesLock.Unlock()

			cmd.client.sendEvent(&bgammon.EventFailedJoin{
				Reason: gotext.GetD(cmd.client.language, "Match not found."),
			})
		case bgammon.CommandLeave, "l":
			if clientGame == nil {
				cmd.client.sendEvent(&bgammon.EventFailedLeave{
					Reason: gotext.GetD(cmd.client.language, "You are not currently in a match."),
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
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "You are not currently in a match."))
				continue
			} else if clientGame.Winner != 0 {
				continue
			}

			if clientGame.Turn != cmd.client.playerNumber {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "It is not your turn."))
				continue
			}

			gameState := &bgammon.GameState{
				Game:         clientGame.Game,
				PlayerNumber: cmd.client.playerNumber,
				Available:    clientGame.LegalMoves(false),
			}
			if !gameState.MayDouble() {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "You may not double at this time."))
				continue
			}

			if clientGame.DoublePlayer != 0 && clientGame.DoublePlayer != cmd.client.playerNumber {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "You do not currently hold the doubling cube."))
				continue
			}

			opponent := clientGame.opponent(cmd.client)
			if opponent == nil {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "You may not double until your opponent rejoins the match."))
				continue
			}

			clientGame.DoubleOffered = true
			clientGame.NextPartialTurn(opponent.playerNumber)

			cmd.client.sendNotice(gotext.GetND(cmd.client.language, "Double offered to opponent (%d point).", "Double offered to opponent (%d points).", int(clientGame.DoubleValue*2), clientGame.DoubleValue*2))
			clientGame.opponent(cmd.client).sendNotice(fmt.Sprintf(gotext.GetND(clientGame.opponent(cmd.client).language, "%s offers a double (%d point).", "%s offers a double (%d points).", int(clientGame.DoubleValue*2)), cmd.client.name, clientGame.DoubleValue*2))

			clientGame.eachClient(func(client *serverClient) {
				if client.json {
					clientGame.sendBoard(client, false)
				}
			})
		case bgammon.CommandResign:
			if clientGame == nil {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "You are not currently in a match."))
				continue
			} else if clientGame.Winner != 0 {
				continue
			}

			opponent := clientGame.opponent(cmd.client)
			if opponent == nil {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "You may not resign until your opponent rejoins the match."))
				continue
			}

			gameState := &bgammon.GameState{
				Game:         clientGame.Game,
				PlayerNumber: cmd.client.playerNumber,
				Available:    clientGame.LegalMoves(false),
			}
			if gameState.MayDecline() {
				clientGame.Winner = opponent.playerNumber
				clientGame.NextPartialTurn(opponent.playerNumber)

				// TODO Remove when most users are running Boxcars v1.4.6+
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "Declined double offer."))
				clientGame.opponent(cmd.client).sendNotice(fmt.Sprintf(gotext.GetD(clientGame.opponent(cmd.client).language, "%s declined double offer."), cmd.client.name))

				clientGame.replay = append(clientGame.replay, []byte(fmt.Sprintf("%d d %d 0", clientGame.Turn, clientGame.DoubleValue*2)))
			} else if gameState.Turn == 0 || gameState.Turn != cmd.client.playerNumber {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "You may not resign until it is your turn."))
				continue
			} else {
				clientGame.Winner = opponent.playerNumber
				clientGame.NextPartialTurn(opponent.playerNumber)

				// TODO Remove when most users are running Boxcars v1.4.6+
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "Resigned."))
				clientGame.opponent(cmd.client).sendNotice(fmt.Sprintf(gotext.GetD(clientGame.opponent(cmd.client).language, "%s resigned."), cmd.client.name))

				clientGame.replay = append(clientGame.replay, []byte(fmt.Sprintf("%d t", cmd.client.playerNumber)))
			}
			clientGame.Ended = time.Now().Unix()

			var reset bool
			if clientGame.Winner == 1 {
				clientGame.Player1.Points = add8(clientGame.Player1.Points, clientGame.DoubleValue)
				reset = clientGame.Player1.Points < clientGame.Points
			} else {
				clientGame.Player2.Points = add8(clientGame.Player2.Points, clientGame.DoubleValue)
				reset = clientGame.Player2.Points < clientGame.Points
			}
			clientGame.addReplayHeader()

			var winEvent *bgammon.EventWin
			if clientGame.Winner != 0 {
				_, err := recordGameResult(clientGame, 4, clientGame.replay)
				if err != nil {
					log.Fatalf("failed to record game result: %s", err)
				}

				winEvent = &bgammon.EventWin{}
				if clientGame.Winner == 1 {
					winEvent.Player = clientGame.Player1.Name
					winEvent.Resigned = clientGame.Player2.Name
				} else {
					winEvent.Player = clientGame.Player2.Name
					winEvent.Resigned = clientGame.Player1.Name
				}
				if clientGame.Points > 1 {
					winEvent.Points = clientGame.DoubleValue
				}
			}

			if reset {
				// Reset game and continue match.
				clientGame.Reset()
				clientGame.replay = clientGame.replay[:0]
			} else {
				// Record match.
				var err error
				winEvent.Rating, err = recordMatchResult(clientGame, matchTypeCasual)
				if err != nil {
					log.Fatalf("failed to record match result: %s", err)
				}
			}

			clientGame.eachClient(func(client *serverClient) {
				if winEvent != nil {
					client.sendEvent(winEvent)
				}
				clientGame.sendBoard(client, false)
			})
		case bgammon.CommandRoll, "r":
			if clientGame == nil {
				cmd.client.sendEvent(&bgammon.EventFailedRoll{
					Reason: gotext.GetD(cmd.client.language, "You are not currently in a match."),
				})
				continue
			} else if clientGame.Winner != 0 {
				continue
			}

			opponent := clientGame.opponent(cmd.client)
			if opponent == nil {
				cmd.client.sendEvent(&bgammon.EventFailedRoll{
					Reason: gotext.GetD(cmd.client.language, "You may not roll until your opponent rejoins the match."),
				})
				continue
			}

			if !clientGame.roll(cmd.client.playerNumber) {
				cmd.client.sendEvent(&bgammon.EventFailedRoll{
					Reason: gotext.GetD(cmd.client.language, "It is not your turn to roll."),
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
						clientGame.sendBoard(client, false)
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
					Reason: gotext.GetD(cmd.client.language, "You are not currently in a match."),
				})
				continue
			} else if clientGame.Winner != 0 {
				clientGame.sendBoard(cmd.client, false)
				continue
			}

			if clientGame.Turn != cmd.client.playerNumber {
				cmd.client.sendEvent(&bgammon.EventFailedMove{
					Reason: gotext.GetD(cmd.client.language, "It is not your turn to move."),
				})
				continue
			}

			opponent := clientGame.opponent(cmd.client)
			if opponent == nil {
				cmd.client.sendEvent(&bgammon.EventFailedMove{
					Reason: gotext.GetD(cmd.client.language, "You may not move until your opponent rejoins the match."),
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
						Reason: gotext.GetD(cmd.client.language, "Illegal move."),
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
					Reason: gotext.GetD(cmd.client.language, "Illegal move."),
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
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "You are not currently in a match."))
				continue
			} else if clientGame.Winner != 0 {
				continue
			}

			if clientGame.Turn != cmd.client.playerNumber {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "It is not your turn."))
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
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "You are not currently in a match."))
				continue
			} else if clientGame.Winner != 0 {
				continue
			}

			opponent := clientGame.opponent(cmd.client)
			if opponent == nil {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "You must wait until your opponent rejoins the match before continuing the game."))
				continue
			}

			if clientGame.DoubleOffered {
				if clientGame.Turn != cmd.client.playerNumber {
					opponent := clientGame.opponent(cmd.client)
					if opponent == nil {
						cmd.client.sendNotice(gotext.GetD(cmd.client.language, "You may not accept the double until your opponent rejoins the match."))
						continue
					}

					clientGame.DoubleOffered = false
					clientGame.DoubleValue = clientGame.DoubleValue * 2
					clientGame.DoublePlayer = cmd.client.playerNumber
					clientGame.NextPartialTurn(opponent.playerNumber)

					cmd.client.sendNotice(gotext.GetD(cmd.client.language, "Accepted double."))
					opponent.sendNotice(fmt.Sprintf(gotext.GetD(opponent.language, "%s accepted double."), cmd.client.name))

					clientGame.replay = append(clientGame.replay, []byte(fmt.Sprintf("%d d %d 1", clientGame.Turn, clientGame.DoubleValue)))
					clientGame.eachClient(func(client *serverClient) {
						clientGame.sendBoard(client, false)
					})
				} else {
					cmd.client.sendNotice(gotext.GetD(cmd.client.language, "Waiting for response from opponent."))
				}
				continue
			} else if clientGame.Turn != cmd.client.playerNumber {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "It is not your turn."))
				continue
			}

			if clientGame.Roll1 == 0 || clientGame.Roll2 == 0 {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "You must roll first."))
				continue
			}

			legalMoves := clientGame.LegalMoves(false)
			if len(legalMoves) != 0 {
				available := bgammon.FlipMoves(legalMoves, cmd.client.playerNumber, clientGame.Variant)
				bgammon.SortMoves(available)
				cmd.client.sendEvent(&bgammon.EventFailedOk{
					Reason: fmt.Sprintf(gotext.GetD(cmd.client.language, "The following legal moves are available: %s"), bgammon.FormatMoves(available)),
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
						Reason: gotext.GetD(cmd.client.language, "Choose which doubles you want for your acey-deucey."),
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
					cmd.client.Terminate(gotext.GetD(cmd.client.language, "Server error"))
					opponent.Terminate(gotext.GetD(opponent.language, "Server error"))
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
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "You are not currently in a match."))
				continue
			} else if clientGame.Winner == 0 {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "The match you are in is still in progress."))
				continue
			} else if clientGame.rematch == cmd.client.playerNumber {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "You have already requested a rematch."))
				continue
			} else if clientGame.client1 == nil || clientGame.client2 == nil {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "Your opponent left the match."))
				continue
			} else if !s.shutdownTime.IsZero() {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "Failed to create match: %s", gotext.GetD(cmd.client.language, "The server is shutting down. Reason: %s", s.shutdownReason)))
				continue
			} else if clientGame.rematch != 0 && clientGame.rematch != cmd.client.playerNumber {
				s.gamesLock.Lock()

				newGame := newServerGame(clientGame.id, clientGame.Variant)
				newGame.name = clientGame.name
				newGame.Points = clientGame.Points
				newGame.password = clientGame.password
				newGame.client1 = clientGame.client1
				newGame.client2 = clientGame.client2
				newGame.spectators = make([]*serverClient, len(clientGame.spectators))
				copy(newGame.spectators, clientGame.spectators)
				newGame.Player1.Name = clientGame.Player1.Name
				newGame.Player2.Name = clientGame.Player2.Name
				newGame.Player1.Rating = clientGame.Player1.Rating
				newGame.Player2.Rating = clientGame.Player2.Rating
				newGame.Player1.Icon = clientGame.Player1.Icon
				newGame.Player2.Icon = clientGame.Player2.Icon
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

				clientGame.opponent(cmd.client).sendNotice(gotext.GetD(clientGame.opponent(cmd.client).language, "Your opponent would like to play again."))
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "Rematch offer sent."))
				continue
			}
		case bgammon.CommandFollow:
			if len(params) < 1 {
				cmd.client.sendNotice("Please specify a player: follow <username>")
				continue
			} else if cmd.client.accountID == 0 {
				cmd.client.sendNotice("Failed to follow player: Please log in before following.")
				continue
			}

			target, err := accountByUsername(string(params[0]))
			if err != nil || target == nil || target.id == 0 {
				cmd.client.sendNotice("Failed to follow player: Invalid username.")
				continue
			} else if target.id == cmd.client.accountID {
				cmd.client.sendNotice("Following yourself will get you nowhere quickly.")
				continue
			}

			err = setAccountFollows(cmd.client.accountID, target.id, true)
			if err != nil {
				cmd.client.sendNotice(fmt.Sprintf("You are already following %s.", target.username))
				continue
			}
			cmd.client.account.follows = append(cmd.client.account.follows, target.id)
			cmd.client.sendNotice(fmt.Sprintf(gotext.GetD(cmd.client.language, "You are now following %s."), target.username))
		case bgammon.CommandUnfollow:
			if len(params) < 1 {
				cmd.client.sendNotice("Please specify a player: unfollow <username>")
				continue
			} else if cmd.client.accountID == 0 {
				cmd.client.sendNotice("Failed to un-follow player: Please log in before un-following.")
				continue
			}

			target, err := accountByUsername(string(params[0]))
			if err != nil || target == nil || target.id == 0 {
				cmd.client.sendNotice("Failed to un-follow player: Invalid username.")
				continue
			} else if target.id == cmd.client.accountID {
				cmd.client.sendNotice("Un-following yourself will get you somewhere slowly.")
				continue
			}

			err = setAccountFollows(cmd.client.accountID, target.id, false)
			if err != nil {
				cmd.client.sendNotice(fmt.Sprintf("You are not following %s.", target.username))
				continue
			}
			cmd.client.account.follows = removeInt(cmd.client.account.follows, target.id)
			cmd.client.sendNotice(fmt.Sprintf(gotext.GetD(cmd.client.language, "You are no longer following %s."), target.username))
		case bgammon.CommandBoard, "b":
			if clientGame == nil {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "You are not currently in a match."))
				continue
			}

			clientGame.sendBoard(cmd.client, false)
		case bgammon.CommandPassword:
			if cmd.client.account == nil {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "Failed to change password: you are logged in as a guest."))
				continue
			} else if len(params) < 2 {
				cmd.client.sendNotice("Please specify your old and new passwords as follows: password <old> <new>")
				continue
			}

			a, err := loginAccount(s.passwordSalt, cmd.client.name, params[0])
			if err != nil || a == nil || a.id == 0 {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "Failed to change password: incorrect existing password."))
				continue
			}

			err = setAccountPassword(s.passwordSalt, a.id, string(bytes.Join(params[1:], []byte("_"))))
			if err != nil {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "Failed to change password."))
				continue
			}
			cmd.client.sendNotice(gotext.GetD(cmd.client.language, "Password changed successfully."))
		case bgammon.CommandSet:
			if len(params) < 2 {
				cmd.client.sendNotice("Please specify the setting name and value as follows: set <name> <value>")
				continue
			}

			name := string(bytes.ToLower(params[0]))
			settings := []string{"autoplay", "highlight", "pips", "moves", "flip", "traditional", "advanced", "mutejoinleave", "mutechat", "muteroll", "mutemove", "mutebearoff", "dim", "speed"}
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
			minValue := 0
			maxValue := 1
			switch name {
			case "dim":
				maxValue = 2
			case "speed":
				maxValue = 3
			}
			if err != nil || value < minValue || value > maxValue {
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
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "Invalid username provided."))
				continue
			} else if clientGame != nil {
				if cmd.client.playerNumber == 1 {
					clientGame.rejoin1 = false
				} else {
					clientGame.rejoin2 = false
				}
				clientGame.removeClient(cmd.client)
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

				ev.Achievements = make([]*bgammon.HistoryAchievement, len(a.achievementIDs))
				for i := range a.achievementIDs {
					ev.Achievements[i] = &bgammon.HistoryAchievement{
						ID:        a.achievementIDs[i],
						Replay:    a.achievementGames[i],
						Timestamp: a.achievementDates[i],
					}
				}
			}
			cmd.client.sendEvent(ev)
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
					cmd.client.sendNotice(gotext.GetD(cmd.client.language, "Invalid replay ID provided."))
					continue
				}
				replay, err = replayByID(id)
				if err != nil {
					cmd.client.sendNotice(gotext.GetD(cmd.client.language, "Invalid replay ID provided."))
					continue
				}
			}
			if len(replay) == 0 {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "No replay was recorded for that game."))
				continue
			} else if clientGame != nil {
				if cmd.client.playerNumber == 1 {
					clientGame.rejoin1 = false
				} else {
					clientGame.rejoin2 = false
				}
				clientGame.removeClient(cmd.client)
			}
			cmd.client.sendEvent(&bgammon.EventReplay{
				ID:      id,
				Content: replay,
			})
		case bgammon.CommandPong:
			// Do nothing.
		case bgammon.CommandDisconnect:
			if clientGame != nil {
				clientGame.removeClient(cmd.client)
			}
			cmd.client.Terminate("Client disconnected")
		case bgammon.CommandMOTD:
			if len(params) == 0 {
				cmd.client.sendNotice("Message of the day:")
				s.sendMOTD(cmd.client)
				continue
			} else if !cmd.client.Admin() {
				cmd.client.sendNotice("Access denied.")
				continue
			}

			motd := bytes.Join(params, []byte(" "))
			if bytes.Equal(bytes.ToLower(motd), clearBytes) {
				motd = nil
			}
			s.motd = string(motd)
			cmd.client.sendNotice("MOTD updated.")
		case bgammon.CommandBroadcast:
			if !cmd.client.Admin() {
				cmd.client.sendNotice("Access denied.")
				continue
			} else if len(params) == 0 {
				cmd.client.sendNotice("Please specify a message to broadcast.")
				continue
			}

			message := string(bytes.Join(params, []byte(" ")))
			s.clientsLock.Lock()
			for _, sc := range s.clients {
				sc.sendBroadcast(message)
			}
			s.clientsLock.Unlock()
		case bgammon.CommandDefcon:
			if len(params) == 0 {
				cmd.client.sendNotice(fmt.Sprintf("Current DEFCON level: %d.", s.defcon))
				continue
			} else if !cmd.client.Admin() && !cmd.client.Mod() {
				cmd.client.sendNotice("Access denied.")
				continue
			}

			v, err := strconv.Atoi(string(params[0]))
			if err != nil || v < 1 || v > 5 {
				cmd.client.sendNotice("Failed to update DEFCON level: invalid level.")
				continue
			} else if v == s.defcon {
				cmd.client.sendNotice("Failed to update DEFCON level: already at specified DEFCON level.")
				continue
			}

			s.defcon = v
			cmd.client.sendNotice(fmt.Sprintf("Updated DEFCON level to %d.", v))

			s.clientsLock.Lock()
			for _, sc := range s.clients {
				sc.sendDefconWarning(s.defcon)
			}
			s.clientsLock.Unlock()
		case bgammon.CommandKick:
			if len(params) == 0 {
				cmd.client.sendNotice("Please specify a username.")
				continue
			} else if !cmd.client.Admin() && !cmd.client.Mod() {
				cmd.client.sendNotice("Access denied.")
				continue
			}

			var reason string
			if len(params) > 1 {
				reason = string(bytes.Join(params[1:], []byte(" ")))
			}

			msg := "Kicked"
			if reason != "" {
				msg += ": " + reason
			}

			var found bool
			nameLower := bytes.ToLower(params[0])
			s.clientsLock.Lock()
			for _, sc := range s.clients {
				if bytes.Equal(bytes.ToLower(sc.name), nameLower) {
					found = true
					sc.Client.Terminate(msg)
					break
				}
			}
			s.clientsLock.Unlock()

			if !found {
				cmd.client.sendNotice("No client was found with that username.")
			} else {
				cmd.client.sendNotice(fmt.Sprintf("Kicked %s.", params[0]))
			}
		case bgammon.CommandBan:
			if len(params) == 0 {
				cmd.client.sendNotice("Please specify an IP address or username.")
				continue
			} else if !cmd.client.Admin() && !cmd.client.Mod() {
				cmd.client.sendNotice("Access denied.")
				continue
			}

			var reason string
			if len(params) > 1 {
				reason = string(bytes.Join(params[1:], []byte(" ")))
			}

			msg := "You are banned"
			if reason != "" {
				msg += ": " + reason
			}

			isIP := bytes.ContainsRune(params[0], '.') || bytes.ContainsRune(params[0], ':')
			if isIP {
				ip := s.hashIP(string(params[0]))
				err := addBan(ip, 0, cmd.client.accountID, reason)
				if err != nil {
					cmd.client.sendNotice("Failed to add ban: " + err.Error())
				}

				s.clientsLock.Lock()
				for _, sc := range s.clients {
					if sc.Address() == ip {
						sc.Client.Terminate(msg)
					}
				}
				s.clientsLock.Unlock()

				cmd.client.sendNotice(fmt.Sprintf("Banned %s.", params[0]))
			} else {
				account, err := accountByUsername(string(params[0]))
				if err != nil {
					cmd.client.sendNotice("Failed to add ban: " + err.Error())
					continue
				} else if account == nil || account.id == 0 {
					var found bool
					nameLower := bytes.ToLower(params[0])
					s.clientsLock.Lock()
					for _, sc := range s.clients {
						if bytes.Equal(bytes.ToLower(sc.name), nameLower) {
							found = true
							err := addBan(sc.Address(), 0, cmd.client.accountID, reason)
							if err != nil {
								cmd.client.sendNotice("Failed to add ban: " + err.Error())
							}
							sc.Client.Terminate(msg)
							break
						}
					}
					s.clientsLock.Unlock()

					if !found {
						cmd.client.sendNotice("No account was found with that username.")
					} else {
						cmd.client.sendNotice(fmt.Sprintf("Banned %s.", params[0]))
					}
					continue
				}

				err = addBan("", account.id, cmd.client.accountID, reason)
				if err != nil {
					cmd.client.sendNotice("Failed to add ban: " + err.Error())
				}

				s.clientsLock.Lock()
				for _, sc := range s.clients {
					if sc.accountID == account.id {
						sc.Client.Terminate(msg)
						break
					}
				}
				s.clientsLock.Unlock()

				cmd.client.sendNotice(fmt.Sprintf("Banned %s.", params[0]))
			}
		case bgammon.CommandUnban:
			if len(params) == 0 {
				cmd.client.sendNotice("Please specify an IP address or username.")
				continue
			} else if !cmd.client.Admin() && !cmd.client.Mod() {
				cmd.client.sendNotice("Access denied.")
				continue
			}

			isIP := bytes.ContainsRune(params[0], '.') || bytes.ContainsRune(params[0], ':')
			if isIP {
				err := deleteBan(s.hashIP(string(params[0])), 0)
				if err != nil {
					cmd.client.sendNotice("Failed to remove ban: " + err.Error())
					continue
				}
			} else {
				account, err := accountByUsername(string(params[0]))
				if err != nil {
					cmd.client.sendNotice("Failed to remove ban: " + err.Error())
					continue
				} else if account == nil || account.id == 0 {
					cmd.client.sendNotice("No account was found with that username.")
					continue
				}
				err = deleteBan("", account.id)
				if err != nil {
					cmd.client.sendNotice("Failed to remove ban: " + err.Error())
					continue
				}
			}
			cmd.client.sendNotice(fmt.Sprintf("Unbanned %s.", params[0]))
		case bgammon.CommandShutdown:
			if !cmd.client.Admin() {
				cmd.client.sendNotice("Access denied.")
				continue
			} else if len(params) < 2 {
				cmd.client.sendNotice("Please specify the number of minutes until shutdown and the reason.")
				continue
			} else if !s.shutdownTime.IsZero() {
				cmd.client.sendNotice("Server shutdown already in progress.")
				continue
			}

			minutes, err := strconv.Atoi(string(params[0]))
			if err != nil || minutes <= 0 {
				cmd.client.sendNotice("Error: Invalid shutdown delay.")
				continue
			}

			s.shutdown(time.Duration(minutes)*time.Minute, string(bytes.Join(params[1:], []byte(" "))))
		case "endgame":
			if !s.debug {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "You are not allowed to use that command."))
				continue
			} else if clientGame == nil {
				cmd.client.sendNotice(gotext.GetD(cmd.client.language, "You are not currently in a match."))
				continue
			}

			clientGame.Turn = 1
			clientGame.Roll1 = 5
			clientGame.Roll2 = 5
			clientGame.Roll3 = 0
			clientGame.Variant = bgammon.VariantBackgammon
			clientGame.Player1.Entered = true
			clientGame.Player2.Entered = true
			clientGame.Board = []int8{0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, -1, 0, 0, 0, 0}

			log.Println(clientGame.Board[0:28])

			clientGame.eachClient(func(client *serverClient) {
				clientGame.sendBoard(client, false)
			})
		default:
			log.Printf("Received unknown command from client %s: %s", cmd.client.label(), cmd.command)
			cmd.client.sendNotice(fmt.Sprintf(gotext.GetD(cmd.client.language, "Unknown command: %s"), cmd.command))
		}
	}
}

func removeInt(s []int, v int) []int {
	for i, sv := range s {
		if sv == v {
			s[i] = s[len(s)-1]
			return s[:len(s)-1]
		}
	}
	return s
}
