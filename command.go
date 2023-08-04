package bgammon

// commands are always sent TO the server

type Command string

const (
	CommandLogin  = "login"  // Log in with username and password, or as a guest.
	CommandSay    = "say"    // Send chat message.
	CommandList   = "list"   // List available games.
	CommandCreate = "create" // Create game.
	CommandJoin   = "join"   // Join game.
	CommandRoll   = "roll"   // Roll dice.
	CommandMove   = "move"   // Move checkers.
)
