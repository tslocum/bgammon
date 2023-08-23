package bgammon

// commands are always sent TO the server

type Command string

const (
	CommandLogin      = "login"      // Log in with username and password, or as a guest.
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
	CommandDisconnect = "disconnect" // Disconnect from server.
)
