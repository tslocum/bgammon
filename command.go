package bgammon

type Command string

const (
	CommandLogin         = "login"         // Log in with username and password, or as a guest.
	CommandLoginJSON     = "loginjson"     // Log in with username and password, or as a guest, and enable JSON messages.
	CommandRegister      = "register"      // Register an account.
	CommandRegisterJSON  = "registerjson"  // Register an account and enable JSON messages.
	CommandResetPassword = "resetpassword" // Request password reset link via email.
	CommandPassword      = "password"      // Change password.
	CommandSet           = "set"           // Change account setting.
	CommandReplay        = "replay"        // Retrieve replay.
	CommandHistory       = "history"       // Retrieve match history.
	CommandHelp          = "help"          // Print help information.
	CommandJSON          = "json"          // Enable or disable JSON formatted messages.
	CommandSay           = "say"           // Send chat message.
	CommandList          = "list"          // List available matches.
	CommandCreate        = "create"        // Create match.
	CommandJoin          = "join"          // Join match.
	CommandLeave         = "leave"         // Leave match.
	CommandDouble        = "double"        // Offer double to opponent.
	CommandResign        = "resign"        // Decline double offer and resign game.
	CommandRoll          = "roll"          // Roll dice.
	CommandMove          = "move"          // Move checkers.
	CommandReset         = "reset"         // Reset checker movement.
	CommandOk            = "ok"            // Confirm checker movement and pass turn to next player.
	CommandRematch       = "rematch"       // Confirm checker movement and pass turn to next player.
	CommandFollow        = "follow"        // Follow a player.
	CommandUnfollow      = "unfollow"      // Un-follow a player.
	CommandBoard         = "board"         // Print current board state in human-readable form.
	CommandPong          = "pong"          // Response to server ping.
	CommandDisconnect    = "disconnect"    // Disconnect from server.
	CommandMOTD          = "motd"          // Read (or write) the message of the day.
	CommandBroadcast     = "broadcast"     // Send a message to all players.
	CommandDefcon        = "defcon"        // Apply restrictions to guests to prevent abuse.
	CommandShutdown      = "shutdown"      // Prevent the creation of new matches.
)

type EventType string

const (
	EventTypeWelcome     = "welcome"
	EventTypeHelp        = "help"
	EventTypePing        = "ping"
	EventTypeNotice      = "notice"
	EventTypeSay         = "say"
	EventTypeList        = "list"
	EventTypeJoined      = "joined"
	EventTypeFailedJoin  = "failedjoin"
	EventTypeLeft        = "left"
	EventTypeFailedLeave = "failedleave"
	EventTypeBoard       = "board"
	EventTypeRolled      = "rolled"
	EventTypeFailedRoll  = "failedroll"
	EventTypeMoved       = "moved"
	EventTypeFailedMove  = "failedmove"
	EventTypeFailedOk    = "failedok"
	EventTypeWin         = "win"
	EventTypeSettings    = "settings"
	EventTypeReplay      = "replay"
	EventTypeHistory     = "history"
)

var HelpText = map[string]string{
	CommandLogin:         "[username] [password] - Log in. A random username is assigned when none is provided.",
	CommandRegister:      "<email> <username> <password> - Register an account. A valid email address must be provided.",
	CommandResetPassword: "<email> - Request a password reset link via email.",
	CommandPassword:      "<old> <new> - Change account password.",
	CommandSet:           "<name> <value> - Change account setting. Available settings: highlight, pips and moves.",
	CommandReplay:        "<id> - Retrieve replay of the specified game.",
	CommandHistory:       "<username> [page] - Retrieve match history of the specified player.",
	CommandHelp:          "[command] - Request help for all commands, or optionally a specific command.",
	CommandSay:           "<message> - Send a chat message. This command can only be used after creating or joining a match.",
	CommandList:          "- List all matches.",
	CommandCreate:        "<public>/<private [password]> <points> <variant> [name] - Create a match. A variant value of 0 represents a standard game, a value of 1 represents an acey-deucey game and a value of 2 represents a tabula game.",
	CommandJoin:          "<id>/<username> [password] - Join match by match ID or by player.",
	CommandLeave:         "- Leave match.",
	CommandDouble:        "- Offer double to opponent.",
	CommandResign:        "- Resign game. Resigning when a double is offered will decline the offer.",
	CommandRoll:          "- Roll dice.",
	CommandMove:          "<from-to> [from-to]... - Move checkers.",
	CommandReset:         "- Reset pending checker movement.",
	CommandOk:            "[1-6] - Accept double offer or confirm checker movement. The parameter for this command only applies in acey-deucey games.",
	CommandRematch:       "- Request (or accept) a rematch after a match has been finished.",
	CommandFollow:        "<username> - Follow a player. A notification is shown whenever a followed player goes online or offline.",
	CommandUnfollow:      "<username> - Un-follow a player.",
	CommandBoard:         "- Request current match state.",
	CommandPong:          "<message> - Sent in response to server ping event to prevent the connection from timing out.",
	CommandDisconnect:    "- Disconnect from the server.",
	CommandMOTD:          "[message] - View (or set) message of the day. Specifying a new message of the day is only available to server administrators.",
	CommandBroadcast:     "<message> - Send a message to all players. This command is only available to server administrators.",
	CommandDefcon:        "[level] - Apply restrictions to guests to prevent abuse. Levels:\n1. Disallow new accounts from being registered.\n2. Only registered users may create and join matches.\n3. Only registered users may chat and set custom match titles.\n4. Warning message is broadcast to all users.\n5. Normal operation.",
	CommandShutdown:      "<minutes> <reason> - Prevent the creation of new matches and periodically warn players about the server shutting down. This command is only available to server administrators.",
}
