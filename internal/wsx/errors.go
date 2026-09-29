package wsx

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Errors returned by this package. Compare with [errors.Is].
var (
	// ErrClosed is returned by operations on a connection that has closed.
	ErrClosed = errors.New("websocket connection is closed")
	// ErrNoURL means the request has no URL.
	ErrNoURL = errors.New("enter a WebSocket URL first")
	// ErrBadScheme means the URL is not ws://, wss://, http:// or https://.
	ErrBadScheme = errors.New("the URL must start with ws:// or wss://")
	// ErrUnsupportedAuth means the auth type cannot be used for a WebSocket
	// handshake (Digest and NTLM need a 401 challenge round trip).
	ErrUnsupportedAuth = errors.New("this auth type cannot be used for a WebSocket handshake")
	// ErrInvalidJSON marks a JSON message that does not parse.
	ErrInvalidJSON = errors.New("the message is not valid JSON")
	// ErrInvalidBase64 marks a binary message that is not base64.
	ErrInvalidBase64 = errors.New("the message is not valid base64")
)

// maxErrorBody bounds how much of a refused handshake's body is kept.
const maxErrorBody = 1024

// HandshakeError is returned by Dial when the server answered the upgrade
// request with something other than 101 Switching Protocols, for example
// 401 Unauthorized.
type HandshakeError struct {
	// Status is the HTTP status code.
	Status int
	// StatusText is the reason phrase without the code.
	StatusText string
	// Header is the response header.
	Header http.Header
	// Body is the first part of the response body (at most 1 KB).
	Body string
}

// Error implements error. It carries the status only, never the URL.
func (e *HandshakeError) Error() string {
	return fmt.Sprintf("the server refused the WebSocket handshake: %d %s", e.Status, e.StatusText)
}

// Is lets errors.Is(err, ErrHandshake) match any handshake failure.
func (e *HandshakeError) Is(target error) bool { return target == ErrHandshake }

// ErrHandshake matches every [*HandshakeError] with errors.Is.
var ErrHandshake = errors.New("websocket handshake refused")

// DialError is a connection failure before or during the handshake that is not
// an HTTP refusal: DNS, refused connection, TLS, timeout. Its message has the
// URL removed.
type DialError struct {
	// Err is the cause with any URL stripped.
	Err error
}

// Error implements error.
func (e *DialError) Error() string { return "could not connect: " + friendly(e.Err) }

// Unwrap returns the cause so errors.Is(err, context.DeadlineExceeded) works.
func (e *DialError) Unwrap() error { return e.Err }

// stripURL removes the URL Go embeds in *url.Error, since it may carry a
// query-string credential.
func stripURL(err error) error {
	var ue *url.Error
	for errors.As(err, &ue) {
		err = ue.Err
		if !errors.As(err, &ue) {
			break
		}
	}
	return err
}

// friendly turns low-level transport errors into short plain text.
func friendly(err error) string {
	err = stripURL(err)
	var (
		unknown x509.UnknownAuthorityError
		host    x509.HostnameError
		inval   x509.CertificateInvalidError
		rh      tls.RecordHeaderError
		dns     *net.DNSError
	)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "the connection timed out"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.As(err, &unknown), errors.As(err, &host), errors.As(err, &inval):
		return "the server certificate is not trusted (" + firstLine(err.Error()) + "). Turn off Verify TLS in Settings to connect anyway"
	case errors.As(err, &rh):
		return "the server did not answer with TLS; check for ws:// versus wss://"
	case errors.As(err, &dns):
		return "could not find host " + dns.Name
	}
	return firstLine(err.Error())
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
