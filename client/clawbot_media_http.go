package client

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

// NewClawBotClientWithHTTPClient permits controlled transports (including local
// protocol fixtures). The caller owns the transport; ordinary production code
// should use NewClawBotClient with its standard TLS configuration.
func NewClawBotClientWithHTTPClient(hc *http.Client, appVersion ...string) *ClawBotClient {
	c := NewClawBotClient(appVersion...)
	if hc != nil {
		c.httpClient = hc
	}
	return c
}

func (c *ClawBotClient) clawBotMediaHTTPClient() (*http.Client, func()) {
	hc := *c.httpClient
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	close := func() {}
	if hc.Transport == nil {
		hc.Transport = http.DefaultTransport
	}
	if transport, ok := hc.Transport.(*http.Transport); ok {
		tr := transport.Clone()
		tr.Proxy = nil
		tr.DialTLS = nil
		tr.DialTLSContext = nil
		tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, errors.New("missing media address")
			}
			for _, ip := range ips {
				if !publicMediaIP(ip.IP) {
					return nil, errors.New("private media address rejected")
				}
			}
			return (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		}
		hc.Transport = tr
		close = tr.CloseIdleConnections
	}
	return &hc, close
}
