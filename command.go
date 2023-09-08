package bgammon

// commands are always sent TO the server

type Command string

const (
	CommandLogin      = "login"      // Log in with username and password, or as a guest.
	CommandLoginJSON  = "loginjson"  // Log in with username and password, or as a guest, and enable JSON messages.
	CommandHelp       = "help"       // Print help information.
	CommandJSON       = "json"       // Enable or disable JSON formatted messages.
	CommandSay        = "say"        // Send chat message.
	CommandList       = "list"       // List available games.
	CommandCreate     = "create"     // Create game.
	CommandJoin       = "join"       // Join game.
	CommandLeave      = "leave"      // Leave game.
	CommandRoll       = "roll"       // Roll dice.
	CommandMove       = "move"       // Move checkers.
	CommandReset      = "reset"      // Reset checker movement.
	CommandOk         = "ok"         // Confirm checker movement and pass turn to next player.
	CommandBoard      = "board"      // Print current board state in human-readable form.
	CommandPong       = "pong"       // Response to server ping.
	CommandDisconnect = "disconnect" // Disconnect from server.
)

type EventType string

const (
	EventTypeWelcome    = "welcome"
	EventTypeHelp       = "help"
	EventTypePing       = "ping"
	EventTypeNotice     = "notice"
	EventTypeSay        = "say"
	EventTypeList       = "list"
	EventTypeJoined     = "joined"
	EventTypeFailedJoin = "failedjoin"
	EventTypeLeft       = "left"
	EventTypeBoard      = "board"
	EventTypeRolled     = "rolled"
	EventTypeFailedRoll = "failedroll"
	EventTypeMoved      = "moved"
	EventTypeFailedMove = "failedmove"
	EventTypeFailedOk   = "failedok"
	EventTypeWin        = "win"
)
