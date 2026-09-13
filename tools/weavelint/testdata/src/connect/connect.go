package connect

type Response[T any] struct{ Msg *T }

type Request[T any] struct{ Msg *T }
