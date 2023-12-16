# Specification of bgammon.org replay file

Replays are stored as .match files.

## Match format (.match)

A .match file contains one or more games concatenated together.

The games are prefixed with a table listing the index of each game.

### Index table

The index table consists of one or more lines in the following format:

`bgammon-replay <index>`

Games are in chronological order.
The index specifies the index of the first byte of the first line of a game.
The index is always eight digits with leading zeroes.

### Game

#### Metadata

The first line of the file specifies the metadata.

`i <player1> <player2> <total> <score1> <score2> <winner> <points> <acey>`

#### Index table

The index table consists of one or more lines in the following format:

`g <player> <index>`

The index specifies the index of the first byte of each line corresponding to each turn in the game.
The index is always eight digits with leading zeroes.

#### Events

##### Double

Accepted:

`d 2 1`

Declined:

`d 2 0`

##### Roll and move

Moves are always specified from player 1's perspective.

`r 5-3 13/8 24/21`
