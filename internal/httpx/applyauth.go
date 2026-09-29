package httpx

import (
	"context"
	"net/http"

	"github.com/shubham/postctl/internal/model"
)

// ApplyAuth applies auth to an outgoing request the way [Client.Send] does
// for a request without a body: it sets headers (Authorization, API-key
// header, AWS Signature v4, Hawk) or appends query parameters (API key or
// OAuth 2.0 access token in the query) on req. Digest is challenge-driven and
// is left untouched, so callers that cannot answer a 401 challenge (for
// example the WebSocket handshake) must check for it themselves.
//
// The auth must already be resolved: no {{variables}}, inheritance flattened.
// source obtains an OAuth 2.0 token when auth holds none; it may be nil.
func ApplyAuth(ctx context.Context, req *http.Request, auth *model.Auth, source TokenSource) error {
	return applyAuth(ctx, req, auth, nil, source)
}
