package integration

func process(s string) string {
	if len(s) > 10 {
		s = s[:10]
	}
	if len(s) > 0 && s[0] == 'x' {
		return "branch-x: " + s
	}
	if len(s) > 1 && s[1] == 'y' {
		return "branch-y: " + s
	}
	return "default: " + s
}
