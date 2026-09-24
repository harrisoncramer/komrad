package constructorsskip

type ServerParams struct {
	Addr string
}

type Server struct {
	addr string
}

func NewServer(p ServerParams) *Server {
	return &Server{addr: p.Addr}
}
