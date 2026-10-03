package require

type TestingT interface {
	Errorf(format string, args ...any)
	FailNow()
}

func Contains(t TestingT, s, contains any, msgAndArgs ...any) {}

func Equal(t TestingT, expected, actual any, msgAndArgs ...any) {}
