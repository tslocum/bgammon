# Specification of bgammon.org protocol

Connect via `bgammon.org:1337` (TCP) or `wss://ws.bgammon.org` (WebSocket).

Replace spaces with underscores when sending a password to the server.

When connected via TCP, commands and events are separated by newlines.

Players always perceive games from the perspective of player number 1 (black).

## User commands

### Format

`command <required argument> [optional argument]`

### Client commands

Clients must send a register command, reset command or login command before sending any other commands.

- `register <email> <username> <password>`
  - Register an account. A valid email address must be provided.
  - Usernames must contain at least one non-numeric character.

- `registerjson <client> <email> <username> <password>`
  - Register an account and enable JSON formatted responses.
  - All client applications should use the `registerjson` command to register, as JSON 
formatted responses are more easily parsed by computers.
  - The client field is specified as follows: `example-client-v1.2.3/en`
  - Aliases: `rj`

- `resetpassword <email>`
  - Request a password reset link via email.
  - This command always terminates the client with the message "resetpasswordok", even if an account is not found.

- `login [username] [password]`
  - Log in. A random username is assigned when none is provided.
  - Usernames must contain at least one non-numeric character.

- `loginjson <client> [username] [password]`
  - Log in and enable JSON formatted responses.
  - All client applications should use the `loginjson` command to log in, as JSON 
formatted responses are more easily parsed by computers.
  - The client field is specified as follows: `example-client-v1.2.3/en`
  - Aliases: `lj`

- `password <old> <new>`
  - Change account password.

- `set <name> <value>`
  - Change account setting.
  - Available settings: `highlight`, `pips` and `moves`.

- `replay <id>`
  - Retrieve replay of the specified game.

- `history <username> [page]`
  - Retrieve match history of the specified player.

- `json <on/off>`
  - Turn JSON formatted messages on or off. JSON messages are not sent by default.

- `help [command]`
  - Request help for all commands, or optionally a specific command.
  - Aliases: `h`

- `list`
  - List all matches.
  - Aliases: `ls`

- `create <public>/<private [password]> <points> <variant> [name]`
  - Create a match. A `variant` value of 0 represents a standard game, a value of 1 represents an acey-deucey game and a value of 2 represents a tabula game.
  - Aliases: `c`

- `join <id>/<username> [password]`
  - Join match by match ID or by player.
  - Aliases: `j`

- `leave`
  - Leave match.

- `double`
  - Offer double to opponent.
  - Aliases: `d`

- `resign`
  - Resign game. Resigning when a double is offered will decline the offer.

- `roll`
  - Roll dice.
  - Aliases: `r`

- `move <from-to> [from-to]...`
  - Move checkers.
  - Aliases: `m`, `mv`

- `reset`
  - Reset pending checker movement.
  - Aliases: `r`

- `ok [1-6]`
  - Accept double offer or confirm checker movement. The parameter for this command only applies in acey-deucey games.
  - In normal games, confirming checker movement passes the turn to the next player.
  - In acey-deucey games, when confirming moves after rolling an acey-deucey, the double roll the player chooses must be specified.
  - Aliases: `k`

- `rematch`
  - Offer (or accept) a rematch after a match has been finished.
  - Aliases: `rm`

- `say <message>`
  - Send a chat message.
  - This command can only be used after creating or joining a match.
  - Aliases: `s`

- `board`
  - Print current match state in human-readable form.
  - This command is not normally used, as the match state is provided in JSON format.
  - Aliases: `b`

- `follow <username>`
  - Follow a player. A notification is shown whenever a followed player goes online or offline.

- `unfollow <username>`
  - Un-follow a player.

- `pong <message>`
  - Sent in response to server `ping` event to prevent the connection from timing out.
  - Whether the client sends a `pong` command, or any other command, clients
must write some data to the server at least once every 40 seconds.

- `disconnect`
  - Disconnect from the server.

- `motd [message]`
  - View (or set) message of the day.
  - Specifying a new message of the day is only available to server administrators.

- `broadcast <message>`
  - Send a message to all players.
  - This command is only available to server administrators.

- `defcon [level]`
  - Apply restrictions to guests to prevent abuse.
  - This command is only available to server administrators and moderators.
  - Levels:
    1. Disallow new accounts from being registered.
    2. Only registered users may connect.
    3. Only registered users may chat and set custom match titles.
    4. Warning message is broadcast to all users.
    5. Normal operation.

- `ban <username> [reason]`
  - Ban a user by IP addresss and account (if logged in).
  - This command is only available to server administrators and moderators.

- `unban <IP>/<username> <reason>`
  - Unban a user by IP address or account.
  - This command is only available to server administrators and moderators.

- `shutdown <minutes> <reason>`
  - Prevent the creation of new matches and periodically warn players about the server shutting down.
  - This command is only available to server administrators.

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
  - Initial welcome message sent only to clients connected via TCP. It provides instructions on how to log in.
  - This message does not normally need to be displayed when using a graphical client.

- `welcome <name:text> there are <clients:integer> clients playing <games:integer> matches.`
  - Initial message sent by the server.

- `notice <message:line>`
  - Server message. This should always be displayed to the user.

- `liststart Matches list:`
  - Start of matches list.

- `game <id:integer> <password:boolean> <points:integer> <players:integer> <name:line>`
  - Match description.

- `listend End of matches list.`
  - End of matches list.

- `failedcreate <message:line>`
  - Sent after failing to create a match.

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
clients must write some data to the server at least once every 40 seconds.
