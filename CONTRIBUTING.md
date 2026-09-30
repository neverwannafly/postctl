# Contributing to postctl

Start with [the first-hour guide](docs/onboarding.md), then use
[the architecture guide](docs/architecture.md) and the focused
[flow walkthroughs](docs/README.md#contributors). Decisions in
`docs/plan.md` §0 are binding. A change to one of them needs the project
owner's approval before implementation.

## Five-minute setup

Prerequisites: macOS (Apple Silicon or Intel), Go 1.26+, Git, and
`golangci-lint`. Install [Task](https://taskfile.dev/) if you want the short
commands below.

```sh
git clone https://github.com/shubham/postctl
cd postctl
go build -o ./bin/postctl ./cmd/postctl

# Use a throwaway copy so the committed example stays unchanged.
tmp="$(mktemp -d)"
cp -R examples/httpbin/. "$tmp/"
./bin/postctl --workspace "$tmp"
```

For UI work without network access, launch the same workspace with the
deterministic fake sender:

```sh
POSTCTL_FAKE_SENDER=1 ./bin/postctl --workspace "$tmp"
```

`--workspace` is important in development: it bypasses upward discovery and
the global `~/.config/postctl` fallback, so experiments cannot affect a real
workspace.

## Development loop

```sh
task build                 # bin/postctl, version embedded
task test                  # go test -race ./...
task lint                  # golangci-lint run
task fmt                   # gofmt -s and linter formatting
task tidy                  # go mod tidy
task assets                # rebuild embedded JS; requires Node/npm

go test ./internal/store -run TestGolden -update
go test ./internal/script -run TestCorpus -update
go generate ./cmd/genkeys
```

Review every generated diff. The storage golden command changes the public
on-disk format; the corpus command changes expected Postman compatibility;
`cmd/genkeys` rewrites `docs/keys.md` from the key registry. Do not hand-edit a
generated result to hide a mismatch.

Prefer focused tests while iterating, then run the complete gate. HTTP tests
use `httptest`, secret tests use `store.MemorySecrets`, and filesystem tests
use `t.TempDir`.

## Definition of done

- The behavior follows `docs/plan.md` §0 and preserves package boundaries.
- New behavior has focused tests, including cancellation and masking where
  relevant.
- Views remain pure: disk, network, scripts, Keychain, and processes run in
  Bubble Tea commands or lower services.
- User-facing keys, flags, errors, and storage changes are documented.
- Generated files and intentional goldens are regenerated and reviewed.
- `go build ./...`, `go test -race ./...`, and `golangci-lint run` pass.
- No secret, real request body, private URL, token, or customer data appears
  in code, fixtures, logs, screenshots, commits, or review text.

## Branches and commits

Use a short branch such as `feat/pager-search`, `fix/oauth-cancel`, or
`docs/runner-flow`. Keep commits reviewable and describe the behavior in the
imperative mood (`mask OAuth tokens in reports`). Separate generated-file
updates when that makes the functional diff easier to review. Do not rewrite
another contributor's branch or ledger entry.

For broad or binding design changes, first record the proposed decision and
ask the owner. `docs/ledger.md` explains historical choices; it is context,
while `docs/plan.md` §0 is the current authority.

## Privacy and test safety

- Use synthetic hosts such as `example.test`, fake tokens, and generated IDs.
- Put optional local acceptance data under `testdata/private/`; it is
  gitignored and tests must skip when it is absent.
- Never call the real macOS Keychain in a test. Inject `store.MemorySecrets`
  through the `store.SecretStore` interface.
- Never copy a real Postman database, environment, headers, body, or console
  output into a fixture or failure message.
- Test extraction against a temporary copy and report only counts and
  structure.
- Mask before data crosses a persistence or presentation boundary. Do not rely
  on callers to clean plaintext after logging or rendering it.

The storage boundary is implemented in `internal/store/secrets.go`
`SecretStore`; the test fake is declared in the same file as `MemorySecrets`.
