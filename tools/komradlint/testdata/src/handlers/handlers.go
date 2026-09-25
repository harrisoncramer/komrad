package handlers

import (
	"connect"
	"errors"
	"rpcerr"
)

type Thing struct{}

type Service struct{}

func (s *Service) GetThing(req *connect.Request[Thing]) (*connect.Response[Thing], error) {
	return rpcerr.Answer(s.getThing(req.Msg))
}

func (s *Service) getThing(_ *Thing) (*Thing, error) {
	return &Thing{}, nil
}

func (s *Service) ListThings(req *connect.Request[Thing]) (*connect.Response[Thing], error) { // want "a connect handler must be one line"
	thing, err := s.getThing(req.Msg)
	if err != nil {
		return nil, errors.New("failed to get the thing")
	}

	return &connect.Response[Thing]{Msg: thing}, nil
}
