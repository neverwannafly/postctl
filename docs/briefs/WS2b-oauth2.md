# WS2b — OAuth 2.0 token flows

Owner model: Grok 4.7 (high, fast) · Branch: `ws/2b-oauth2` · Worktree: `~/work/postctl-wt/ws2b-oauth2`

## Goal

`internal/oauth2`: fetch OAuth 2.0 tokens for a request's `model.OAuth2Config`, as Postman's "Get New Access Token" does. Grants in scope (plan §0 R2): **client credentials**, **authorization code with PKCE** (and without PKCE), **password**, **implicit**. No automatic refresh in v1; the user triggers a fetch. Refreshing with a refresh token on demand is allowed.

## Read first

- `docs/plan.md` §0 (R2, D2), `docs/model.md` rules (esp. rule 6 on OAuth token secrets).
- `internal/model/auth.go` (`OAuth2Config`, `OAuth2Token`), `internal/store` (`SecretStore`, `HydrateAuth`).
- `internal/httpx/types.go` (contract). WS2 will add a `TokenSource` interface to httpx: `Token(ctx context.Context, cfg *model.OAuth2Config) (*model.OAuth2Token, error)`. Implement that exact signature; don't import httpx's implementation.
- Reference for the loopback pattern: `~/work/prod-console-tui/internal/auth/login.go` (browser open via `/usr/bin/open` with argv, loopback listener on `127.0.0.1`, random path, state check, timeout, prompt close). Reuse the pattern; don't copy Okta-specific logic.

## Requirements

1. `Fetch(ctx, cfg *model.OAuth2Config, opts FetchOptions) (*model.OAuth2Token, error)` dispatching by grant. `cfg` values are already variable-resolved by the caller.
2. Client authentication: `client_secret_basic` (header) and `client_secret_post` (body), per `cfg`. Scopes, audience, resource and extra params from `cfg` (use `Raw`/extra maps for Postman fields that aren't modelled).
3. Auth code (+PKCE S256/plain): loopback listener on `127.0.0.1:<port>`. If the configured redirect URI is a localhost URL, honor its port and path; otherwise use Postman's approach: an ephemeral port, plus a note in the result that the provider must allow the loopback URI. Validate `state` with a constant-time compare. Browser opening goes behind a `Browser` interface (`OpenURL(string) error`); the default is `/usr/bin/open`. Return the auth URL through a callback so the TUI can show a copyable link. Include timeout and cancellation. Close the listener on every exit path. Show a small, calm HTML page in the browser on success or failure.
4. Implicit: same loopback. Tokens arrive in the URL fragment, so serve a tiny page whose JS posts the fragment back to the loopback server.
5. Password grant: straightforward token request.
6. Parse token responses (JSON and form-encoded) into `model.OAuth2Token` (access, refresh, id token, type, expiry from `expires_in`, scope). Map error responses (`error`, `error_description`) to a typed `*Error` with a user-readable message.
7. `TokenSource` implementation `NewSource(secrets store.SecretStore, fetch func…)` that returns the cached token when present and not expired. Otherwise it returns `ErrNoToken`; it does **not** auto-fetch, because the TUI/CLI decides when to fetch (R2). Provide `Save(token)` that persists token strings through the `SecretStore`, following model rule 6.
8. Never log or return token values in error strings.

## Tests

A fake authorization server with `httptest` covering every grant, both client-auth styles, PKCE verification, state mismatch, provider error responses, timeouts and cancellation (and the listener really closes), and form-encoded responses. The browser is a fake that performs the redirect by calling the loopback URL.

## Acceptance

`go build ./... && go test -race ./... && golangci-lint run` green; ledger entries in your worktree.

## Parallel agents

WS0.1, WS1, WS2 (`internal/vars`, `internal/httpx`), WS3, WS5 are running. Only create files under `internal/oauth2/`.

## Final message

Exported API, how the TUI should drive the auth-code flow (sequence of calls and callbacks), deviations, test/lint results, commit hash.
