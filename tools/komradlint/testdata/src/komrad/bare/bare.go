package bare

import (
	"net/http"
	"time"
)

var client = http.DefaultClient // want `http.DefaultClient has no timeouts; build the client with xhttp.NewClient`

func transport() http.RoundTripper {
	return http.DefaultTransport // want `http.DefaultTransport has no timeouts; build the client with xhttp.NewClient`
}

func literal() *http.Client {
	return &http.Client{Timeout: time.Second} // want `a bare http.Client has no timeouts; build it with xhttp.NewClient or xhttp.NewTransport`
}

func transportLiteral() *http.Transport {
	return &http.Transport{} // want `a bare http.Transport has no timeouts; build it with xhttp.NewClient or xhttp.NewTransport`
}

func fine(c *http.Client, r *http.Request) (*http.Response, error) {
	return c.Do(r)
}
