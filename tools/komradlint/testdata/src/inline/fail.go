package inline

// fail stands in for komrad's own package, which the testdata cannot import.
var fail = struct {
	NotFound        func(string) *sentinel
	InvalidArgument func(string) *sentinel
	Because         func(error, string) *sentinel
}{
	NotFound:        func(message string) *sentinel { return &sentinel{} },
	InvalidArgument: func(message string) *sentinel { return &sentinel{} },
	Because:         func(err error, message string) *sentinel { return &sentinel{} },
}

type sentinel struct{}

func (s *sentinel) Error() string { return "" }

func (s *sentinel) Saying(format string, args ...any) *sentinel { return s }
