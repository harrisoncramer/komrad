package domain

// fail stands in for weave's own package, which the testdata cannot import.
var fail = struct {
	NotFound func(string) error
}{
	NotFound: func(message string) error { return nil },
}
