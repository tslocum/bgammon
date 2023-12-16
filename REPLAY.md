# Specification of bgammon.org replay file

Replays are stored as .match files.

## Match format (.match)

A .match file contains one or more games concatenated together.

The games are prefixed with a table listing the index of each game.

### Index table

The index table consists of one or more lines in the following format:

`bgammon-replay <index>`

Games are in chronological order.
The index specifies the position of the first byte of the first line of a game.
The index is always eight digits in length with leading zeroes.

### Game

#### Metadata

The first line of the game is the metadata.

`i <player1> <player2> <total> <score1> <score2> <winner> <points> <acey>`

#### Events

The remaining lines of the game are the events.

Events are in the following format:

`<player> <event>`

##### Double

Accepted:

`1 d 2 1`

Declined:

`1 d 2 0`

##### Roll and move

Moves are always specified from player 1's perspective.

`1 r 5-3 13/8 24/21`
