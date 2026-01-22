package tls

import (
	"compress/flate"
	"compress/gzip"
	"context"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"

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

// Fingerprint represents a browser TLS fingerprint
type Fingerprint string

const (
	FingerprintFirefox Fingerprint = "firefox"
	FingerprintChrome  Fingerprint = "chrome"
)

// NewClient returns a new http.Client that uses utls to mimic a real browser for TLS.
// The client automatically handles response decompression for gzip, deflate, br, and zstd.
// It supports both HTTP/1.1 and HTTP/2 via ALPN negotiation.
func NewClient(fingerprint Fingerprint) *http.Client {
	var clientHello utls.ClientHelloID
	switch fingerprint {
	case FingerprintChrome:
		clientHello = utls.HelloChrome_Auto
	default: // Firefox is default
		clientHello = utls.HelloFirefox_Auto
	}

	transport := &http.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{
				Timeout: 30 * time.Second,
			}
			conn, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}

			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				host = addr
			}

			uConn := utls.UClient(conn, &utls.Config{
				ServerName: host,
			}, clientHello)

			if err := uConn.Handshake(); err != nil {
				_ = conn.Close()
				return nil, err
			}

			return uConn, nil
		},
	}

	return &http.Client{
		Transport: &decompressingTransport{rt: transport},
		Timeout:   30 * time.Second,
	}
}
