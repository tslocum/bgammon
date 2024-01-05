# Specification of bgammon.org replay file

Replays are stored as .match files with lines separated by newline characters only
(no carriage-return characters). Replay files are always UTF-8 encoded.

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

The first line of the game is the metadata. The timestamp specifies when the game started.

`i <timestamp> <player1> <player2> <total> <score1> <score2> <winner> <points> <acey>`

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

Moves for both players are specified from player 1's perspective. The highest roll value is specified first.

`1 r 5-3 13/8 24/21`

When no moves are possible, only the roll is specified.

`1 r 4-4`

## Example .match file

```
bgammon-replay 00000024
i 1702738670 Guest_385 BOT_tabula 1 0 0 1 1 0
1 r 4-1 13/9 9/8
2 r 6-1 1/7 1/2
1 r 6-5 13/7 7/2
2 r 3-3 bar/3 bar/3 12/15 12/15
1 r 6-2 8/2 8/6
2 r 5-2 12/14 14/19
1 r 4-4 24/20 20/16 24/20 20/16
2 r 5-4 3/7 7/12
1 r 5-3 16/13 13/8
2 r 5-1 3/4 4/9
1 r 2-1 16/14 14/13
2 r 5-1 9/10 10/15
1 r 1-1 6/5 6/5 8/7 8/7
2 r 6-2 19/21 15/21
1 r 4-4 13/9 13/9 13/9 13/9
2 r 5-1 15/20 12/13
1 r 6-4 9/3 7/3
2 r 5-2 17/19 15/20
1 r 6-2 7/1 6/4
2 r 5-4 17/22 17/21
1 r 4-3 8/5 5/1
2 r 3-1 12/15 12/13
1 r 6-3 9/3 9/6
2 r 4-2 15/19 13/15
1 r 3-1 9/8 8/5
2 r 5-2 15/20 13/15
1 r 6-5 5/off 6/off
2 r 3-1 15/16 16/19
1 r 5-1 5/off 1/off
2 r 3-1 22/off 19/20
1 r 6-5 6/off 5/off
2 r 4-4 21/off 21/off 21/off 19/23
1 r 5-1 6/5 5/off
2 r 6-5 20/off 19/off
1 r 6-3 6/off 3/off
2 r 3-2 23/off 19/22
1 r 5-4 4/off 3/off
2 r 4-2 19/23 23/off
1 r 6-3 3/off 2/off
2 r 5-1 20/off 19/20
1 r 2-1 1/off 2/off
```
