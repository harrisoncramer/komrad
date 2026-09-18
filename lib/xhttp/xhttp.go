// Package xhttp holds the one shape of HTTP call weave makes to a JSON API.
package xhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// DefaultMaxBytes bounds a response body when a caller names no cap.
const DefaultMaxBytes = 8 << 20

// JSONParams says what to send and how far to read the answer.
type JSONParams struct {
	// Client makes the call, NewClient(DefaultTimeout) when nil.
	Client *http.Client
	Method string
	URL    string
	// Header is set on the request as given, ahead of Accept and Content-Type.
	Header http.Header
	// Body is encoded as JSON and sent when not nil.
	Body any
	// MaxBytes bounds the answer read, DefaultMaxBytes when zero.
	MaxBytes int64
}

// Answer is what came back, with the body read out and closed.
type Answer struct {
	Status int
	Body   []byte
}

// IsSuccess says whether the status is in the 2xx range.
func (a Answer) IsSuccess() bool {
	return a.Status >= 200 && a.Status < 300
}

// JSON makes the call, reads and closes the body, decodes it into out on a 2xx, and answers
// with the status and body whatever they were.
func JSON(ctx context.Context, params JSONParams, out any) (Answer, error) {
	client := params.Client
	if client == nil {
		client = NewClient(DefaultTimeout)
	}
	maxBytes := params.MaxBytes
	if maxBytes == 0 {
		maxBytes = DefaultMaxBytes
	}

	var payload io.Reader
	if params.Body != nil {
		encoded, err := json.Marshal(params.Body)
		if err != nil {
			return Answer{}, fmt.Errorf("failed to encode the request for %s: %w", params.URL, err)
		}

		payload = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, params.Method, params.URL, payload)
	if err != nil {
		return Answer{}, fmt.Errorf("failed to build the request for %s: %w", params.URL, err)
	}
	for key, values := range params.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	if params.Body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return Answer{}, fmt.Errorf("failed to reach %s: %w", params.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return Answer{}, fmt.Errorf("failed to read the answer from %s: %w", params.URL, err)
	}

	answer := Answer{Status: resp.StatusCode, Body: body}
	if !answer.IsSuccess() || out == nil {
		return answer, nil
	}

	if err := json.Unmarshal(body, out); err != nil {
		return answer, fmt.Errorf("failed to decode the answer from %s: %w", params.URL, err)
	}

	return answer, nil
}

// DefaultTimeout bounds a whole call made through a client that names no timeout of its own.
const DefaultTimeout = 30 * time.Second

// dialTimeout bounds the TCP connect.
const dialTimeout = 10 * time.Second

// tlsHandshakeTimeout bounds the TLS handshake, which the default transport leaves unbounded.
const tlsHandshakeTimeout = 10 * time.Second

// responseHeaderTimeout bounds the wait for the first byte of a response after the request is
// written.
const responseHeaderTimeout = 30 * time.Second

// idleConnTimeout is how long a kept-alive connection waits for reuse before it is closed.
const idleConnTimeout = 90 * time.Second

// NewTransport is the transport every client to an outside service sits on, with the
// handshake and header waits bounded that the standard library's default leaves open.
func NewTransport() *http.Transport {
	return newTransport(responseHeaderTimeout)
}

// NewLocalTransport is the transport a client to weave's own server sits on, with the
// handshake bounded and the header wait left open.
func NewLocalTransport() *http.Transport {
	return newTransport(0)
}

func newTransport(headerTimeout time.Duration) *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: headerTimeout,
		ExpectContinueTimeout: time.Second,
		IdleConnTimeout:       idleConnTimeout,
		MaxIdleConns:          100,
		ForceAttemptHTTP2:     true,
	}
}

// NewClient is a client over NewTransport whose whole call is bounded by timeout, or only by
// the transport's own waits when timeout is zero, which is what a call that streams wants.
func NewClient(timeout time.Duration) *http.Client {
	return NewClientOver(NewTransport(), timeout)
}

// NewLocalClient is a client over NewLocalTransport with no whole-call timeout.
func NewLocalClient() *http.Client {
	return NewClientOver(NewLocalTransport(), 0)
}

// NewClientOver is a client over a round tripper of the caller's own, bounded the same way
// NewClient is.
func NewClientOver(transport http.RoundTripper, timeout time.Duration) *http.Client {
	return &http.Client{Transport: transport, Timeout: timeout}
}
