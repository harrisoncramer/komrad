package params

type FetchParams struct {
	URL string
}

func Fetch(params FetchParams) string {
	return params.URL
}

type redrawParams struct {
	Width int
}

func redraw(params redrawParams) int {
	return params.Width
}

type CreateAgentParams struct {
	Name string
}

func createAgent(params CreateAgentParams) string {
	return params.Name
}

type SendParams struct {
	To string
}

type Mailer struct{}

func (m *Mailer) Send(params SendParams) string {
	return params.To
}

type HoldParams struct {
	ID string
}

func (m *Mailer) Hold(params HoldParams) string {
	return params.ID
}

type Store[T any] struct{}

type PutParams struct {
	Key string
}

type Box struct{}

func (s *Store[T]) Put(params PutParams) string { // want "Put is out of place: declare PutParams directly above Put"
	return params.Key
}

type ServeParams struct {
	Addr string
}

type Server struct{}

func Serve(params ServeParams) string { // want "Serve is out of place: declare ServeParams directly above Serve"
	return params.Addr
}

type WatchParams struct {
	Every int
}

const pollEvery = 2

func Watch(params WatchParams) int { // want "Watch is out of place: declare WatchParams directly above Watch"
	return params.Every * pollEvery
}

func forwardFetch(params FetchParams) string {
	return Fetch(params)
}

type Engine struct{}

func (e *Engine) Fetch(params FetchParams) string {
	return Fetch(params)
}

func (e *Engine) Record(params RecordParams) string { // want "Record is out of place: declare RecordParams directly above Record"
	return params.Name
}

var (
	_ Server
	_ Box
)

func (m *Mailer) Diff(params DiffParams) string {
	return params.Base
}

type narrowedParams[T any] struct {
	Items []T
}

func narrowed[T any](params narrowedParams[T]) []T {
	return params.Items
}

type pickedParams[T any] struct {
	Items []T
}

type unit int

func picked[T any](params pickedParams[T]) []T { // want "picked is out of place: declare pickedParams directly above picked"
	return params.Items
}

type LoadParams struct {
	Path string
}

type format int

func Load(params *LoadParams) string { // want "Load is out of place: declare LoadParams directly above Load"
	return params.Path
}

var (
	_ unit
	_ format
)

type GetParams struct {
	Key string
}

type Cache[T any] struct{}

func (c *Cache[T]) Get(params GetParams) string {
	return params.Key
}

type LookupParams struct {
	Key string
}

type Index[K comparable, V any] struct{}

func (i Index[K, V]) Lookup(params LookupParams) string {
	return params.Key
}
