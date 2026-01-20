package tls

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/http2"

	utls "github.com/refraction-networking/utls"
)

// match firefox fingerprint for now
func NewClient() *http.Client {
	return &http.Client{
		Transport: &http2.Transport{
			DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
				dialer := net.Dialer{Timeout: 30 * time.Second}
				conn, err := dialer.DialContext(context.Background(), network, addr)
				if err != nil {
					return nil, err
				}

				host, _, err := net.SplitHostPort(addr)
				if err != nil {
					host = addr
				}

				uConn := utls.UClient(conn, &utls.Config{
					ServerName: host,
					NextProtos: []string{"h2", "http/1.1"},
				}, utls.HelloFirefox_Auto)

				if err := uConn.Handshake(); err != nil {
					_ = conn.Close()
					return nil, err
				}

				return uConn, nil
			},
		},
		Timeout: 30 * time.Second,
	}
}
