package bgammon

type Player struct {
	Number int // 1 black, 2 white
	Name   string
	Points int
}

func NewPlayer(number int) Player {
	return Player{
		Number: number,
	}
}
