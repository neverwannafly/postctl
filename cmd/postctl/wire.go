package main

import (
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/oauth2"
	"github.com/shubham/postctl/internal/store"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/cookies"
	"github.com/shubham/postctl/internal/tui/envedit"
	"github.com/shubham/postctl/internal/tui/fakesend"
	"github.com/shubham/postctl/internal/tui/request"
)

// newSender supplies the send pipeline for the Request pane. It is a fake
// that plays a plausible exchange until the engine (WS4) lands; replace this
// one function with the engine adapter.
func newSender() tui.Sender { return fakesend.New() }

// wireEditors fills in the screens and panes this workstream owns without
// overwriting anything the caller already set (the response pane is wired in
// openUI).
func wireEditors(o *tui.Options) {
	if o.Sender == nil {
		o.Sender = newSender()
	}
	if o.NewRequestPane == nil {
		o.NewRequestPane = request.Factory(request.Deps{
			Tokens: oauth2.NewSource(store.NewKeychain(""), nil),
		})
	}
	if o.NewEnvEditor == nil {
		o.NewEnvEditor = envedit.Factory(envedit.Deps{})
	}
	if o.NewCookieManager == nil && o.Backend != nil {
		// A workspace whose cookie file cannot be read still opens: the
		// manager is simply not offered.
		if jar, err := httpx.NewCookieJar(o.Backend.Root()); err == nil {
			o.NewCookieManager = cookies.Factory(jar)
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
