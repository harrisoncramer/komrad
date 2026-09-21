package constructors

type NewClientParams struct {
	Addr string
}

type Client struct {
	addr string
}

func NewClient(p NewClientParams) *Client {
	return &Client{addr: p.Addr}
}

type NewHandlerParams struct {
	Client *Client
}

func NewHandler(p NewHandlerParams) func() *Client {
	return func() *Client { return p.Client }
}

type ServerParams struct {
	Addr string
}

type Server struct {
	addr string
}

func NewServer(p ServerParams) *Server { // want "NewServer takes ServerParams, and a constructor's params struct is named NewServerParams"
	return &Server{addr: p.Addr}
}

type Pane struct {
	id string
}

type NewPaneParams struct {
	ID string
}

func NewPane(p NewPaneParams) *Pane { // want "NewPane is declared right after Pane, which is declared right after NewPaneParams"
	return &Pane{id: p.ID}
}

type NewSyncerParams struct {
	Every int
}

type Syncer struct {
	every int
}

type tick int

func NewSyncer(p NewSyncerParams) *Syncer { // want "NewSyncer is declared right after Syncer, which is declared right after NewSyncerParams"
	return &Syncer{every: p.Every}
}

type Registry struct{}

// NewAgentParams is a method's params, which follow the method rule rather than this one.
type NewAgentParams struct {
	Name string
}

func (r *Registry) NewAgent(p NewAgentParams) string {
	return p.Name
}

func NewRegistry() *Registry {
	return &Registry{}
}

var _ tick
