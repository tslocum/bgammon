package bgammon

type Player struct {
	Number   int8 // 1 black, 2 white
	Name     string
	Rating   int
	Points   int8
	Entered  bool // Whether all checkers have entered the board. (Acey-deucey)
	Inactive int  // Inactive time. (Seconds)
	Icon     int  // Profile icon.
}

func NewPlayer(number int8) Player {
	return Player{
		Number: number,
	}
}
