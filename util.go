package bgammon

func minInt(a int, b int) int {
	if b < a {
		return b
	}
	return a
}

func maxInt(a int, b int) int {
	if b > a {
		return b
	}
	return a
}
