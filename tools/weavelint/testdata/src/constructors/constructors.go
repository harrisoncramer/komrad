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

func NewServer(p ServerParams) *Server { // want "NewServer takes ServerParams: name it NewServerParams"
	return &Server{addr: p.Addr}
}

type Pane struct {
	id string
}

type NewPaneParams struct {
	ID string
}

func NewPane(p NewPaneParams) *Pane { // want "NewPane is out of order: declare NewPaneParams, then Pane, then NewPane, one after another"
	return &Pane{id: p.ID}
}

type NewSyncerParams struct {
	Every int
}

type Syncer struct {
	every int
}

type tick int

func NewSyncer(p NewSyncerParams) *Syncer { // want "NewSyncer is out of order: declare NewSyncerParams, then Syncer, then NewSyncer, one after another"
	return &Syncer{every: p.Every}
}

type NewLoggerParams struct {
	Name string
}

type level int

func NewLogger(p NewLoggerParams) func() string { // want "NewLogger is out of order: declare NewLoggerParams directly above NewLogger"
	return func() string { return p.Name }
}

type Registry struct{}

// NewAgentParams is a method's params.
type NewAgentParams struct {
	Name string
}

func (r *Registry) NewAgent(p NewAgentParams) string {
	return p.Name
}

func NewRegistry() *Registry {
	return &Registry{}
}

var (
	_ tick
	_ level
)
