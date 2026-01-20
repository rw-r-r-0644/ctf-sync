package tls

import (
	"compress/flate"
	"compress/gzip"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"golang.org/x/net/http2"

	utls "github.com/refraction-networking/utls"
)

// decompressingTransport wraps an http.RoundTripper and automatically decompresses responses
type decompressingTransport struct {
	rt http.RoundTripper
}

func (t *decompressingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.rt.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	// Decompress based on Content-Encoding
	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body = &readCloser{Reader: gr, Closer: resp.Body}
		resp.Header.Del("Content-Encoding")
		resp.ContentLength = -1
		resp.Uncompressed = true
	case "deflate":
		resp.Body = &readCloser{Reader: flate.NewReader(resp.Body), Closer: resp.Body}
		resp.Header.Del("Content-Encoding")
		resp.ContentLength = -1
		resp.Uncompressed = true
	case "br":
		resp.Body = &readCloser{Reader: brotli.NewReader(resp.Body), Closer: resp.Body}
		resp.Header.Del("Content-Encoding")
		resp.ContentLength = -1
		resp.Uncompressed = true
	case "zstd":
		zr, err := zstd.NewReader(resp.Body)
		if err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body = &readCloser{Reader: zr, Closer: resp.Body}
		resp.Header.Del("Content-Encoding")
		resp.ContentLength = -1
		resp.Uncompressed = true
	}

	return resp, nil
}

// readCloser combines an io.Reader and io.Closer
type readCloser struct {
	io.Reader
	Closer io.Closer
}

func (rc *readCloser) Close() error {
	return rc.Closer.Close()
}

// NewClient returns a new http.Client that uses utls to mimic a real browser (Firefox).
// This helps bypass WAFs that fingerprint the standard Go TLS client.
// The client automatically handles response decompression for gzip, deflate, br, and zstd.
func NewClient() *http.Client {
	return &http.Client{
		Transport: &decompressingTransport{
			rt: &http2.Transport{
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
		},
		Timeout: 30 * time.Second,
	}
}
