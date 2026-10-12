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

// NewTransport is the transport komrad's clients call the server over, with no header wait on a call that awaits hooks.
func NewTransport() http.RoundTripper {
	return hookAwareTransport{plain: xhttp.NewTransport(), awaiting: xhttp.NewStreamTransport()}
}

// RoundTrip sends the request over the transport its procedure calls for.
func (t hookAwareTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if awaitsHooks[trimToProcedure(req.URL.Path)] {
		return t.awaiting.RoundTrip(req)
	}

	return t.plain.RoundTrip(req)
}

func trimToProcedure(urlPath string) string {
	dir, method := path.Split(urlPath)

	return "/" + path.Base(dir) + "/" + method
}
