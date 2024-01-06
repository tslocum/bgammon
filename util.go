package bgammon

func minInt(a int8, b int8) int8 {
	if b < a {
		return b
	}
	return a
}

func maxInt(a int8, b int8) int8 {
	if b > a {
		return b
	}
	return a
}
