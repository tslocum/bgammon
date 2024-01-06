package bgammon

type Player struct {
	Number  int8 // 1 black, 2 white
	Name    string
	Points  int8
	Entered bool // Whether all checkers have entered the board. (Acey-deucey)
}

func NewPlayer(number int8) Player {
	return Player{
		Number: number,
	}
}
