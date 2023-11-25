package bgammon

type Player struct {
	Number  int // 1 black, 2 white
	Name    string
	Points  int
	Entered bool // Whether all checkers have entered the board. (Acey-deucey)
}

func NewPlayer(number int) Player {
	return Player{
		Number: number,
	}
}
