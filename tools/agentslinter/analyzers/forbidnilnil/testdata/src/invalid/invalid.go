package invalid

import "errors"

type agent struct{}

func find() (*agent, error) {
	return nil, nil // want `do not return nil, nil; return a sentinel error the caller can match with errors.Is`
}

func findWithBranch(ok bool) (*agent, error) {
	if !ok {
		return nil, nil // want `do not return nil, nil; return a sentinel error the caller can match with errors.Is`
	}

	return &agent{}, nil
}

func findTwo() (*agent, *agent, error) {
	return nil, nil, nil // want `do not return nil, nil; return a sentinel error the caller can match with errors.Is`
}

func lookup() func() (*agent, error) {
	return func() (*agent, error) {
		return nil, nil // want `do not return nil, nil; return a sentinel error the caller can match with errors.Is`
	}
}

type store struct{}

func (store) get() (*agent, error) {
	return nil, nil // want `do not return nil, nil; return a sentinel error the caller can match with errors.Is`
}

var errMissing = errors.New("missing")

func aliased() (*agent, error) {
	e := errMissing
	if e != nil {
		return nil, e
	}

	return nil, nil // want `do not return nil, nil; return a sentinel error the caller can match with errors.Is`
}
