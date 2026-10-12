package rpcapi

import (
	"net/http"
	"path"

	"komrad/lib/xhttp"
)

type hookAwareTransport struct {
	plain    http.RoundTripper
	awaiting http.RoundTripper
}

// NewTransport is the transport komrad's clients call the server over, which waits on a call that
// awaits hooks for as long as the connection lives and on any other for xhttp's usual bounds.
func NewTransport() http.RoundTripper {
	return hookAwareTransport{plain: xhttp.NewTransport(), awaiting: xhttp.NewStreamTransport()}
}

// RoundTrip sends the request over the transport its procedure calls for.
func (t hookAwareTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if AwaitsHooks(procedureOf(req.URL.Path)) {
		return t.awaiting.RoundTrip(req)
	}

	return t.plain.RoundTrip(req)
}

func procedureOf(urlPath string) string {
	dir, method := path.Split(urlPath)

	return "/" + path.Base(dir) + "/" + method
}
