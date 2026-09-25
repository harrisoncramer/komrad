package handlersskip

import (
	"connect"
	"errors"
)

type Thing struct{}

type Service struct{}

func (s *Service) ListThings(_ *connect.Request[Thing]) (*connect.Response[Thing], error) {
	return nil, errors.New("a handler answering for itself, which the rule is turned off for")
}
