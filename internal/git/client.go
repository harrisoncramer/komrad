package git

// Client is git as a dependency a caller can take.
type Client struct{}

// NewClient returns a client that runs git in whichever directory a call names.
func NewClient() *Client {
	return &Client{}
}
