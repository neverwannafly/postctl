package main

import (
	"context"
	"os"
	"sync"

	"github.com/shubham/postctl/internal/engine"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/oauth2"
	"github.com/shubham/postctl/internal/store"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/cookies"
	"github.com/shubham/postctl/internal/tui/envedit"
	"github.com/shubham/postctl/internal/tui/fakesend"
	"github.com/shubham/postctl/internal/tui/request"
	"github.com/shubham/postctl/internal/tui/tuiengine"
)

type storeProvider interface {
	Store() (*store.Store, error)
}

type lazyEngine struct {
	mu      sync.Mutex
	backend tui.Backend
	store   *store.Store
	engine  *engine.Engine
}

func (p *lazyEngine) get() (*engine.Engine, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.engine != nil {
		return p.engine, nil
	}
	provider, ok := p.backend.(storeProvider)
	if !ok {
		return nil, store.ErrNotWorkspace
	}
	st, err := provider.Store()
	if err != nil {
		return nil, err
	}
	p.store = st
	secrets := st.SecretStore()
	p.engine, err = engine.New(engine.Config{
		Store:   st,
		Secrets: secrets,
		HTTP:    httpx.NewClient(version),
		OAuth2:  oauth2.NewSource(secrets, oauth2.Fetch),
		Version: version,
	})
	return p.engine, err
}

func (p *lazyEngine) secrets() (store.SecretStore, error) {
	if _, err := p.get(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.store.SecretStore(), nil
}

type lazySender struct{ engine *lazyEngine }

func (s *lazySender) Send(ctx context.Context, req tui.SendRequest, progress func(tui.SendStep)) tui.SendResult {
	eng, err := s.engine.get()
	if err != nil {
		result := tui.SendResult{Err: err}
		if req.Request != nil {
			result.RequestID = req.Request.ID
		}
		return result
	}
	return tuiengine.New(eng).Send(ctx, req, progress)
}

type lazySecrets struct{ engine *lazyEngine }

func (s lazySecrets) store() (store.SecretStore, error) {
	return s.engine.secrets()
}

func (s lazySecrets) Get(ctx context.Context, ref model.SecretRef) (string, error) {
	secrets, err := s.store()
	if err != nil {
		return "", err
	}
	return secrets.Get(ctx, ref)
}

func (s lazySecrets) Set(ctx context.Context, ref model.SecretRef, value string) error {
	secrets, err := s.store()
	if err != nil {
		return err
	}
	return secrets.Set(ctx, ref, value)
}

func (s lazySecrets) Delete(ctx context.Context, ref model.SecretRef) error {
	secrets, err := s.store()
	if err != nil {
		return err
	}
	return secrets.Delete(ctx, ref)
}

// wireEditors fills in the screens and panes this workstream owns without
// overwriting anything the caller already set (the response pane is wired in
// openUI).
func wireEditors(o *tui.Options) {
	engines := &lazyEngine{backend: o.Backend}
	if o.Sender == nil {
		if os.Getenv("POSTCTL_FAKE_SENDER") == "1" {
			o.Sender = fakesend.New()
		} else {
			o.Sender = &lazySender{engine: engines}
		}
	}
	if o.NewRequestPane == nil {
		o.NewRequestPane = request.Factory(request.Deps{
			Tokens: oauth2.NewSource(lazySecrets{engine: engines}, nil),
		})
	}
	if o.NewEnvEditor == nil {
		o.NewEnvEditor = envedit.Factory(envedit.Deps{})
	}
	if o.NewCookieManager == nil && o.Backend != nil {
		o.NewCookieManager = func(env tui.ScreenEnv) tui.Screen {
			eng, err := engines.get()
			if err != nil {
				return cookies.New(env, nil)
			}
			return cookies.New(env, eng.Cookies())
		}
	}
}

func init() {
	base := launchUI
	launchUI = func(o tui.Options) error {
		wireEditors(&o)
		return base(o)
	}
}
