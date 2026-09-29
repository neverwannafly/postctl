package oauth2

import (
	"errors"
	"net/url"
	"os/exec"
	"runtime"
)

// Browser opens an authorization URL for the user.
type Browser interface {
	// OpenURL opens raw in a browser. The URL does not contain a client
	// secret, a PKCE verifier, or a token.
	OpenURL(raw string) error
}

// BrowserFunc adapts a function to [Browser].
type BrowserFunc func(string) error

// OpenURL calls f.
func (f BrowserFunc) OpenURL(raw string) error {
	if f == nil {
		return errors.New("oauth2: no browser")
	}
	return f(raw)
}

// SystemBrowser opens http and https URLs with /usr/bin/open on macOS.
// The URL is one argv element, not a shell command.
type SystemBrowser struct{}

// OpenURL implements [Browser].
func (SystemBrowser) OpenURL(raw string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("oauth2: opening a browser is supported on macOS only")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("oauth2: refusing to open a non-http URL")
	}
	cmd := exec.Command("/usr/bin/open", u.String())
	if err := cmd.Start(); err != nil {
		return errors.New("oauth2: could not open the system browser")
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
