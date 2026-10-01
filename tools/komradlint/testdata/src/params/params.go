package params

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

type serveParams struct {
	Addr string
}

func serve(p serveParams) string {
	return p.Addr
}

type watchParams struct {
	Every int
}

type stream int

func watch(p watchParams) int { // want "watch is out of order: declare watchParams directly above watch"
	return p.Every
}

type ConnectParams struct {
	Token string
}

type Service struct{}

func (s *Service) Connect(p ConnectParams) string {
	return p.Token
}

func (s *Service) Hold(p HoldParams) bool { // want "Hold is out of order: declare HoldParams directly above Hold"
	return p.Until > 0
}

type HoldParams struct {
	Until int
}

type Store struct{}

func (s Store) Record(p RecordParams) string { // want "Record is out of order: declare RecordParams directly above Record"
	return p.Name
}

type RecordParams struct {
	Name string
}

func (s *Service) forward(p ConnectParams) string {
	return p.Token
}

type Engine struct{}

func (e *Engine) Diff(p DiffParams) string {
	return p.Base
}

var _ stream
