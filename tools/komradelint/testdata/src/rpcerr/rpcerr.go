package rpcerr

import "connect"

func Answer[T any](message *T, err error) (*connect.Response[T], error) {
	return nil, err
}
