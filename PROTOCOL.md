# Specification of bgammon.org protocol

Connect to `bgammon.org:1337` via TCP.

All commands and events are separated by newlines.

## User commands

### Format

`command <required argument> [optional argument]`

### Commands

- `login [username] [password]`
  - Log in to bgammon. A random username is assigned when none is provided.
  - This must be the first command sent when a client connects to bgammon.
  - *Aliases:* `l`

- `loginjson [username] [password]`
  - Log in to bgammon and enable JSON formatted responses.
  - All client applications should use the `loginjson` command to log in, as JSON 
formatted responses are more easily parsed by computers.
  - *Aliases:* `lj`

- `json <on/off>`
  - Turn JSON formatted messages on or off. JSON messages are not sent by default.

- `help [command]`
  - Request help for all commands, or optionally a specific command.
  - *Aliases:* `h`

- `list`
  - List all games.
  - *Aliases:* `ls`

- `create <public/private> [password]`
  - List all games.
  - *Aliases:* `c`

- `join <id> [password]`
  - Join game.
  - *Aliases:* `j`

- `roll`
  - Roll dice.
  - *Aliases:* `r`

- `move <from-to> [from-to]...`
  - Move checkers.
  - *Aliases:* `m`, `mv`

- `reset`
  - Reset pending checker movement.
  - *Aliases:* `r`

- `ok`
  - Confirm checker movement and pass turn to next player.
  - *Aliases:* `k`

- `say <message>`
  - Send a chat message.
  - This command can only be used after creating or joining a game.
  - *Aliases:* `s`

- `board`
  - Print current game state in human-readable form.
  - This command is not normally used, as the game state is provided in JSON format.
  - *Aliases:* `b`

- `disconnect`
  - Disconnect from the server.

## Events (server responses)

All events are sent in either JSON or human-readable format. The structure of
messages sent in JSON format is available via [godoc](https://docs.rocket9labs.com/code.rocket9labs.com/tslocum/bgammon/#Event).

This document lists events in human-readable format.

### Data types

- `integer` a whole number
- `boolean` - `0` (representing false) or `1` (representing true)
- `text` - alphanumeric without spaces
- `line` - alphanumeric with spaces

### Events

- `hello <message:line>`
  - Initial welcome message sent by the server. It provides instructions on how to log in.
  - This message does not normally need to be displayed when using a graphical client.

- `welcome <name:text> there are <clients:integer> clients playing <games:integer> games.`
  - Initial message sent by the server.

- `notice <message:line>`
  - Server message. This should always be displayed to the user.

- `liststart Games list:`
  - Start of games list.

- `game <id:integer> <password:boolean> <players:integer> <name:line>`
  - Game description.

- `listend End of games list.`
  - End of games list.

- `joined <id:integer> <player:text>`
  - Sent after successfully creating or joining a game, and when another player
joins a game you are in. 
  - The server will always send a `board` response immediately after `joined` to
provide clients with the initial game state.

- `failedjoin <message:line>`
  - Sent after failing to join a game.

- `parted <gameID:integer> <gameID:integer>`
  - Sent after leaving a game.

- `json <message:line>`
  - Server confirmation of client requested JSON formatting.
  - This message does not normally need to be displayed when using a graphical client.

- `failedok <reason:line>`
  - Sent after sending `ok` when there are one or more legal moves still available to the player.
  - Players must make moves using all available dice rolls before ending their turn.

- `say <player:text> <message:line>`
  - Chat message from another player.
