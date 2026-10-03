package assert

type TestingT interface {
	Errorf(format string, args ...any)
}

type Assertions struct{}

func Contains(t TestingT, s, contains any, msgAndArgs ...any) bool { return true }

func Containsf(t TestingT, s, contains any, msg string, args ...any) bool { return true }

func NotContains(t TestingT, s, contains any, msgAndArgs ...any) bool { return true }

func Equal(t TestingT, expected, actual any, msgAndArgs ...any) bool { return true }

func Equalf(t TestingT, expected, actual any, msg string, args ...any) bool { return true }

func (a *Assertions) Contains(s, contains any, msgAndArgs ...any) bool { return true }
