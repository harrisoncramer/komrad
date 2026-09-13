package domain

import "errors"

var ErrPlain = errors.New("this one says nothing about its kind") // want "a sentinel here must say what kind of failure it is"

var (
	ErrOne = errors.New("nor does this one") // want "a sentinel here must say what kind of failure it is"
	ErrTwo = fail.NotFound("this one does")
)

func raise() error {
	return errors.New("a throwaway inside a function is not a sentinel")
}
