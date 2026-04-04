package client

import (
	"net"
	"net/http"
	"time"
)

const (
	chatRequestTimeout   = 2 * time.Minute
	streamRequestTimeout = 10 * time.Minute

	maxErrorBodyBytes  = 1 << 20 // 1MB
	maxStreamLineBytes = 2 << 20 // 2MB
)

func newHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 5 * time.Minute,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   streamRequestTimeout,
	}
}

// NewHTTPClient returns the shared HTTP client configuration used by CornerStone clients.
// It is safe to reuse across requests.
func NewHTTPClient() *http.Client {
	return newHTTPClient()
}
