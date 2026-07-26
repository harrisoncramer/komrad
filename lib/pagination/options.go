package pagination

// The limits a Paginator applies when a listing does not set its own. The
// default is small enough that a caller who asks for nothing gets a page it can
// read rather than the whole store, and the ceiling is what one request may
// pull however large a limit it sends.
const (
	defaultLimit int64 = 50
	maxLimit     int64 = 500
)

// options holds the limits a Paginator enforces.
type options struct {
	defaultLimit int64
	maxLimit     int64
}

// Option configures a Paginator's limits.
type Option func(*options)

// WithDefaultLimit sets the limit used when a request omits one.
func WithDefaultLimit(limit int64) Option {
	return func(o *options) { o.defaultLimit = limit }
}

// WithMaxLimit caps the limit a request may ask for.
func WithMaxLimit(limit int64) Option {
	return func(o *options) { o.maxLimit = limit }
}
