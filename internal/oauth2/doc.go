// Package oauth2 fetches OAuth 2.0 tokens for a request's [model.OAuth2Config],
// the way Postman's "Get New Access Token" does.
//
// [Fetch] runs the grant the caller asked for: client credentials, authorization
// code with or without PKCE, resource-owner password, or implicit. It never
// refreshes a token on its own. Pass [FetchOptions.Refresh] to exchange a stored
// refresh token when the user asks for that. Browser grants listen on
// 127.0.0.1 and open the authorization URL through a [Browser].
//
// [Source.Token] returns a cached token that is still valid, or [ErrNoToken].
// It does not call the fetch function. The TUI or CLI decides when to fetch
// (plan §0 R2). [Source.Save] writes the access, refresh, and ID token strings
// to a [store.SecretStore] as JSON under the token's secret ref. Workspace
// files keep only that ref (model rule 6). The JSON keys match what
// store.HydrateAuth reads back.
//
// Token values are never written to logs or to error strings.
package oauth2
