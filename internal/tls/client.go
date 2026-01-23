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

// Fingerprint represents a browser TLS fingerprint
type Fingerprint string

const (
	FingerprintFirefox Fingerprint = "firefox"
	FingerprintChrome  Fingerprint = "chrome"
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

// hybridTransport routes requests to the appropriate transport based on scheme.
// This allows supporting both plain HTTP (via http.Transport) and
// TLS/HTTP2 with utls (via http2.Transport), which is necessary because
// standard http.Transport cannot handle utls's negotiated HTTP/2 connections correctly.
type hybridTransport struct {
	httpTransport  *http.Transport
	httpsTransport *http2.Transport
}

func (t *hybridTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == "https" {
		return t.httpsTransport.RoundTrip(req)
	}
	return t.httpTransport.RoundTrip(req)
}

// NewClient returns a new http.Client that uses utls to mimic a real browser for TLS.
// It uses a hybrid transport approach:
// - HTTPS: http2.Transport with utls (for WAF bypass and correct H2 handling)
// - HTTP: Standard http.Transport
// The client automatically handles response decompression for gzip, deflate, br, and zstd.
func NewClient(fingerprint Fingerprint) *http.Client {
	var clientHello utls.ClientHelloID
	switch fingerprint {
	case FingerprintChrome:
		clientHello = utls.HelloChrome_Auto
	default: // Firefox is default
		clientHello = utls.HelloFirefox_Auto
	}

	hybrid := &hybridTransport{
		// Standard HTTP transport for plain HTTP (e.g., localhost)
		httpTransport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: 30 * time.Second,
			}).DialContext,
		},
		// HTTP/2 + utls transport for HTTPS (WAF bypass)
		httpsTransport: &http2.Transport{
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
				}, clientHello)

				if err := uConn.Handshake(); err != nil {
					_ = conn.Close()
					return nil, err
				}

				return uConn, nil
			},
		},
	}

	return &http.Client{
		Transport: &decompressingTransport{rt: hybrid},
		Timeout:   30 * time.Second,
	}
}
