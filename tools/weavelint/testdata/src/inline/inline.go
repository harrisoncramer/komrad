package inline

var ErrNoHost = fail.InvalidArgument("a host is needed")

var errNotCursor = fail.NotFound("not a cursor")

func raise(host string) error {
	if host == "" {
		return ErrNoHost
	}

	return fail.InvalidArgument("not a way to connect").Saying("%s is not a way to connect", host) // want `fail.InvalidArgument inside a function is a sentinel nobody can match`
}

func lookup(cursor string) error {
	if cursor == "" {
		return errNotCursor.Saying("%q is not a cursor", cursor)
	}

	return fail.Because(errNotCursor, "the cursor could not be read")
}

func (s *sentinel) Missing() error {
	return fail.NotFound("nothing here") // want `fail.NotFound inside a function is a sentinel nobody can match`
}

var later = func() error {
	return nil
}
