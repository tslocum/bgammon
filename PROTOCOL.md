# Secification of bgammon protocol

## User commands

Format: `command <required argument> [optional argument]`

### `login [username] [password]`

Log in to bgammon. A random username is assigned when none is provided.

This must be the first command sent when a client connects to bgammon.

### `help [command]`

Request help for all commands, or optionally a specific command.

### `list`

List all games.

### `create [public/private] [password]`

List all games.

### `join [ID] [password]`

Join game.

### `roll`

Roll dice.

### `move <from-to> [from-to]...`

Move checkers.

### `reset`

Reset pending checker movement.

### `ok`

Confirm checker movement and pass turn to next player.

### `disconnect`

Disconnect from the server.

## Server responses

Data types:

- `integer` a whole number
- `boolean` - `0` (representing false) or `1` (representing true)
- `text` - alphanumeric without spaces
- `line` - alphanumeric with spaces

### `hello <message:line>`

Initial welcome message sent by the server. It provides instructions on how to log in.

This message does not normally need to be displayed when using a graphical client.

### `welcome <name:text> there are <clients:integer> clients playing <games:integer> games.`

Initial message sent by the server.

### `notice <message:line>`

Server message. This should always be displayed to the user.

### `liststart Games list:`

Start of games list.

### `game <ID:integer> <password:boolean> <players:integer> <name:line>`

Game description.

### `listend End of games list.`

End of games list.