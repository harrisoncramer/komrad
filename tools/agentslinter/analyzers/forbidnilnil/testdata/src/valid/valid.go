package valid

import "errors"

type agent struct{}

var errNotFound = errors.New("not found")

// A sentinel error says why nothing came back, which is the shape this analyzer asks for.
func find() (*agent, error) {
	return nil, errNotFound
}

// A nil slice or map already reads as empty, so nothing is lost by returning one.
func list() ([]*agent, error) {
	return nil, nil
}

func index() (map[string]*agent, error) {
	return nil, nil
}

// An interface result is how the analysis and encoding APIs are shaped, so it is left alone.
func any_() (any, error) {
	return nil, nil
}

// A pointer result with no error alongside it is a plain optional.
func maybe() *agent {
	return nil
}

func pair() (*agent, *agent) {
	return nil, nil
}

// A named result returned bare is not the pattern either, since the values may have been set.
func named() (found *agent, err error) {
	return
}

func inner() func() ([]*agent, error) {
	return func() ([]*agent, error) {
		return nil, nil
	}
}
