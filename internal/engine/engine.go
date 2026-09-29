package engine

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/oauth2"
	"github.com/shubham/postctl/internal/script"
	"github.com/shubham/postctl/internal/store"
)

// HistoryRecorder receives one entry per request that was attempted.
// [history.Log] implements it.
type HistoryRecorder interface {
	// Append records e and returns it with its ID assigned.
	Append(e history.Entry) (history.Entry, error)
}

// Config wires an [Engine]. Only Store is required.
type Config struct {
	// Store is the workspace. Required.
	Store *store.Store
	// Secrets holds secret variable values and OAuth 2.0 tokens. It
	// defaults to Store.SecretStore().
	Secrets store.SecretStore
	// HTTP sends resolved requests. It defaults to httpx.NewClient(Version).
	HTTP httpx.Sender
	// Cookies is the workspace cookie jar. It defaults to
	// httpx.NewCookieJar(Store.Root()).
	Cookies *httpx.CookieJar
	// OAuth2 supplies stored tokens. It defaults to an oauth2.Source over
	// Secrets. It never fetches a token.
	OAuth2 httpx.TokenSource
	// History records sent requests. It defaults to a log under
	// <workspace>/.postctl/history; set DisableHistory to turn it off.
	History HistoryRecorder
	// DisableHistory turns history off when History is nil.
	DisableHistory bool
	// Version is put in the default User-Agent.
	Version string
	// Clock returns the current time. It defaults to time.Now.
	Clock func() time.Time
	// ScriptTimeout bounds each script run. Zero means script.DefaultTimeout.
	ScriptTimeout time.Duration
	// MaxBodyBytes caps the response body kept in memory. Zero means the
	// httpx default.
	MaxBodyBytes int64
	// Defaults are the workspace-wide request settings passed to httpx.
	Defaults model.RequestSettings
}

// Engine runs the send pipeline and the collection runner for one workspace.
// It is safe for concurrent use: any number of sends and runs may overlap,
// and their variable writes are merged rather than overwritten.
type Engine struct {
	cfg     Config
	ws      *workspace
	cookies script.CookieAccess
	nextID  atomic.Uint64
}

// New builds an Engine, filling in defaults for every field of cfg except
// Store. It returns an error when Store is nil or the cookie jar or history
// directory cannot be opened.
func New(cfg Config) (*Engine, error) {
	if cfg.Store == nil {
		return nil, errors.New("engine: Config.Store is required")
	}
	if cfg.Secrets == nil {
		cfg.Secrets = cfg.Store.SecretStore()
	}
	if cfg.HTTP == nil {
		cfg.HTTP = httpx.NewClient(cfg.Version)
	}
	if cfg.Cookies == nil {
		jar, err := httpx.NewCookieJar(cfg.Store.Root())
		if err != nil {
			return nil, fmt.Errorf("engine: open cookie jar: %w", err)
		}
		cfg.Cookies = jar
	}
	if cfg.OAuth2 == nil {
		cfg.OAuth2 = oauth2.NewSource(cfg.Secrets, oauth2.Fetch)
	}
	if cfg.History == nil && !cfg.DisableHistory {
		log, err := history.Open(filepath.Join(cfg.Store.StateDir(), "history"), history.Options{})
		if err != nil {
			return nil, fmt.Errorf("engine: open history: %w", err)
		}
		cfg.History = log
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	return &Engine{
		cfg:     cfg,
		ws:      workspaceFor(cfg.Store.Root()),
		cookies: NewCookieAccess(cfg.Cookies),
	}, nil
}

// Cookies returns the cookie jar the engine sends with, for the cookie
// manager.
func (e *Engine) Cookies() *httpx.CookieJar { return e.cfg.Cookies }

// httpOptions are the per-send options shared by the main request and
// pm.sendRequest.
func (e *Engine) httpOptions() httpx.Options {
	return httpx.Options{
		WorkspaceDir: e.cfg.Store.Root(),
		MaxBodyBytes: e.cfg.MaxBodyBytes,
		Defaults:     e.cfg.Defaults,
		OAuth2:       e.cfg.OAuth2,
		CookieJar:    e.cfg.Cookies,
	}
}
