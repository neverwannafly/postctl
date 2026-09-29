# WS2 — Variables and HTTP engine

Owner model: GPT-5.6 Sol (medium) · Branch: `ws/2-vars-httpx` · Worktree: `~/work/postctl-wt/ws2-vars-httpx`

## Goal

Two packages that everything else sends through:

- `internal/vars`: implements the `vars.Store` contract in `internal/vars/scope.go`, plus request resolution.
- `internal/httpx`: implements `httpx.Sender` from `internal/httpx/types.go`, plus auth application and a persistent cookie jar.

The contract files were written by the orchestrator. **Extend them additively only.** WS3 (script runtime) and WS4 (engine) are coding against them right now.

## Read first

`docs/plan.md` §0 (binding: R2, R3, C1), §4.1 (send pipeline); `docs/model.md` rules; `internal/model` (`Request`, `URL`, `Body`, `Auth`, `RequestSettings`, `Variable`, `Environment`, `Globals`, `Cookie`); `internal/store` (`SecretStore`, `Hydrate*`, `.postctl/` local state helpers).

## `internal/vars`

1. `New(globals, collection, environment []model.Variable, data map[string]any) *MemStore` implementing `Store`; disabled variables are ignored; insertion order is kept. Build the collection layer from the collection variables only (folder-level variables don't exist in Postman v2.1).
2. `Replace`: `{{name}}` with nested resolution (a value may reference others; guard cycles with a depth limit of 16 and leave the reference unresolved); whitespace inside braces as Postman handles it; unknown names left literal. Stringify non-strings like Postman: objects/arrays become JSON, numbers use the shortest form, and nil becomes an empty string.
3. Dynamic variables: `$guid`, `$randomUUID`, `$timestamp`, `$isoTimestamp`, `$randomInt`, plus the common `$random*` faker set Postman documents (`$randomEmail`, `$randomFirstName`, `$randomAlphaNumeric`, `$randomBoolean`, `$randomColor`, `$randomIP`, `$randomUserName`, `$randomPassword`, `$randomPhoneNumber`, `$randomCity`, `$randomCountry`, `$randomCompanyName`, `$randomUrl`, `$randomWord(s)`, `$randomLoremSentence`, `$randomDateFuture/Past/Recent`…). Implement a table so more can be added. Each occurrence gets a fresh value. The random source is injectable for tests.
4. `ResolveRequest(req *model.Request, chain []model.Container, s Store) (*model.Request, error)`: deep-copy the request (use `Clone` if WS0.1 has landed; otherwise a local copy helper you delete later), flatten inherited auth (request → folders → collection; nil means inherit), substitute variables in URL raw + query + path variables (`:id`), headers, all body modes (including form-data keys/values and file paths), and auth fields. Return a request with `Auth` never inherit.
5. `Changes()` records per-scope mutations for persistence (R3). Also add `Apply(ws *model.Workspace, env *model.Environment, col *model.Collection, changes)`, a helper that writes changes back into the model values. The engine will call it and then persist through the store. Values for secret vars stay secret.

## `internal/httpx`

1. `Client` implementing `Sender`: `net/http` with a per-send transport configured from `RequestSettings` (redirect policy incl. max hops, original method, auth header on cross-host; TLS verify; timeout; HTTP/2 enabled). Timings via `net/http/httptrace`. Record redirect hops. Decompress gzip/deflate/br (br via `github.com/andybalholm/brotli`). Enforce `MaxBodyBytes` (default 50 MiB). Wire-size accounting.
2. Bodies: raw with a Content-Type derived from `Language` unless a header is set; urlencoded; multipart form-data with files (paths relative to `WorkspaceDir`); binary file streaming; GraphQL as JSON `{query, variables}`.
3. Auth application for `bearer`, `basic`, `apikey` (header or query), `digest` (challenge-response retry), `awsv4` (SigV4 signing: implement it, or use `github.com/aws/aws-sdk-go-v2/aws/signer/v4` if it's small enough), `hawk`, `ntlm` (best effort; return a clear "unsupported" error if too costly). For `oauth2`, just add the stored token as `Authorization: <tokenType> <token>` or as a query param. Fetching tokens is WS2b's job; define `type TokenSource interface { Token(ctx, *model.OAuth2Config) (*model.OAuth2Token, error) }` in httpx and use it if one is provided in `Options` (add an additive `Options.OAuth2 TokenSource` field).
4. Cookie jar: `CookieJar` persisted to the workspace's `.postctl/cookies.json` (public-suffix aware via `golang.org/x/net/publicsuffix`), with list/delete/clear per domain for the TUI cookie manager. Exposed through `Options` (additive field). The script runtime will need `Get(url)`/`Set` for `pm.cookies`, so export those.
5. Default headers like Postman: `User-Agent: postctl/<version>`, `Accept: */*`, `Accept-Encoding: gzip, deflate, br`, `Connection: keep-alive` (only when not user-set; user-disabled headers really stay off).
6. `Curl(req *model.Request) string` and `CurlFromSent(SentRequest) string` for copy-as-curl (plan Q8 codegen). Output must be shell-safe (single-quote escaping).

## Tests

`httptest` servers for every body mode, auth type (with known vectors: SigV4 test suite vectors, the RFC 7616 digest example), redirects, compression, truncation, timeouts (the transport error must be an error, not a Response), cookie persistence round-trip, curl escaping. `vars` tests for precedence, nesting, cycles, dynamic variables (deterministic random), and stringification.

## Acceptance

`go build ./... && go test -race ./... && golangci-lint run` green; ledger entries in your worktree; contract files changed additively only (say what you added in the DONE entry).

## Parallel agents

WS0.1 (model/store), WS1 (importer), WS2b (`internal/oauth2`, implements your `TokenSource`), WS3 (`internal/script`, consumes your contracts and needs `CookieJar` + `Sender`), WS5 (TUI). Don't edit their packages.

## Final message

Exported API of both packages, contract additions, deviations, open questions, test/lint results, commit hash.
