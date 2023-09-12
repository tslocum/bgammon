# Specification of bgammon.org protocol

Connect to `bgammon.org:1337` via TCP.

All commands and events are separated by newlines.

## User commands

### Format

`command <required argument> [optional argument]`

### Client commands

- `login [username] [password]`
  - Log in to bgammon. A random username is assigned when none is provided.
  - Usernames must contain at least one non-numeric character.
  - This (or `loginjson`) must be the first command sent when a client connects to bgammon.
  - Aliases: `l`

- `loginjson [username] [password]`
  - Log in to bgammon and enable JSON formatted responses.
  - All client applications should use the `loginjson` command to log in, as JSON 
formatted responses are more easily parsed by computers.
  - Aliases: `lj`

- `json <on/off>`
  - Turn JSON formatted messages on or off. JSON messages are not sent by default.

- `help [command]`
  - Request help for all commands, or optionally a specific command.
  - Aliases: `h`

- `list`
  - List all matches.
  - Aliases: `ls`

- `create <public/private> [password]`
  - Create a match.
  - Aliases: `c`

- `join <id>/<username> [password]`
  - Join match by match ID or by player.
  - Aliases: `j`

- `roll`
  - Roll dice.
  - Aliases: `r`

- `move <from-to> [from-to]...`
  - Move checkers.
  - Aliases: `m`, `mv`

- `reset`
  - Reset pending checker movement.
  - Aliases: `r`

- `ok`
  - Confirm checker movement and pass turn to next player.
  - Aliases: `k`

- `rematch`
  - Request (or accept) a rematch after a match has been finished.
  - Aliases: `rm`

- `say <message>`
  - Send a chat message.
  - This command can only be used after creating or joining a match.
  - Aliases: `s`

- `board`
  - Print current match state in human-readable form.
  - This command is not normally used, as the match state is provided in JSON format.
  - Aliases: `b`

- `pong <message>`
  - Sent in response to server `ping` event to prevent the connection from timing out.
  - Whether the client sends a `pong` command, or any other command, clients
must write some data to the server at least once every ten minutes.

- `disconnect`
  - Disconnect from the server.

## Server events

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

- `welcome <name:text> there are <clients:integer> clients playing <games:integer> matches.`
  - Initial message sent by the server.

- `notice <message:line>`
  - Server message. This should always be displayed to the user.

- `liststart Matches list:`
  - Start of matches list.

- `game <id:integer> <password:boolean> <players:integer> <name:line>`
  - Match description.

- `listend End of matches list.`
  - End of matches list.

- `joined <id:integer> <playerNumber:integer> <playerName:text>`
  - Sent after successfully creating or joining a match, and when another player
joins a match you are in.
  - The server will always send a `board` event immediately after `joined` to
provide clients with the initial match state.

- `failedjoin <message:line>`
  - Sent after failing to join a match.

- `left <username:text>`
  - Sent after leaving a match.

- `json <message:line>`
  - Server confirmation of client requested JSON formatting.
  - This message does not normally need to be displayed when using a graphical client.

- `failedok <reason:line>`
  - Sent after sending `ok` when there are one or more legal moves still available to the player.
  - Players must make moves using all available dice rolls before ending their turn.

- `win <player:text> wins!`
  - Sent after a player bears their final checker off the board.

- `say <player:text> <message:line>`
  - Chat message from another player.

- `ping <message:text>`
  - Sent to clients to prevent their connection from timing out.
  - Whether the client replies with a `pong` command, or any other command,
clients must write some data to the server at least once every ten minutes.