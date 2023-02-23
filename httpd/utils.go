package httpd

func assert1(b bool, s string) {
	if !b {
		panic(s)
	}
}
