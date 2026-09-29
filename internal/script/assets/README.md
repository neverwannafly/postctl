# Embedded sandbox assets

Everything the goja sandbox loads lives here and is compiled into the binary
with `go:embed` (see `assets.go`). Nothing is fetched or built at run time:
postctl never shells out to node.

- `bundles/` — the npm libraries scripts can `require`, as pinned, minified
  UMD bundles. Committed.
- `sandbox/` — the `pm.*` implementation, hand-written for this sandbox. The
  files are concatenated in file-name order inside one IIFE, so the numeric
  prefixes are the load order, not decoration.
- `package.json`, `package-lock.json`, `build.js` — the development-time build.

## Rebuilding the bundles

```sh
task assets      # npm ci && node build.js, from this directory
```

`build.js` browserifies each library standalone — which shims the Node core
modules that `xml2js` and friends reach for — and minifies the result with
terser targeting ES5, the dialect goja is happiest with. It writes
`bundles/<name>.js` and `bundles/versions.json`.

Node and npm are development-time tools only. Check the diff after a rebuild:
the bundles are the sandbox's supply chain, and a new transitive dependency
can quietly pull in a browser or Node API that goja does not have.

## What is bundled, and why

| Library | Version | License | Source | Why |
| --- | --- | --- | --- | --- |
| `lodash` | 4.17.21 | MIT | [lodash/lodash](https://github.com/lodash/lodash) | By far the most-required library in real scripts. |
| `moment` | 2.30.1 | MIT | [moment/moment](https://github.com/moment/moment) | Date arithmetic in pre-request scripts. |
| `crypto-js` | 4.2.0 | MIT | [brix/crypto-js](https://github.com/brix/crypto-js) | HMAC request signing, digests, AES. |
| `uuid` | 8.3.2 | MIT | [uuidjs/uuid](https://github.com/uuidjs/uuid) | Correlation and idempotency identifiers. |
| `chai` | 4.4.1 | MIT | [chaijs/chai](https://github.com/chaijs/chai) | `pm.expect` is chai; the assertion messages must match. |
| `chai-postman` | 2.0.1 | Apache-2.0 | [postmanlabs/chai-postman](https://github.com/postmanlabs/chai-postman) | Postman's own plugin: `.to.have.status`, `.to.be.ok`, `.jsonSchema`. |
| `ajv` | 6.12.6 | MIT | [ajv-validator/ajv](https://github.com/ajv-validator/ajv) | JSON Schema, and what chai-postman validates with. |
| `tv4` | 1.3.0 | Public Domain / MIT | [geraintluff/tv4](https://github.com/geraintluff/tv4) | The older schema validator, still used by long-lived collections. |
| `xml2js` | 0.6.2 | MIT | [Leonidas-from-XIV/node-xml2js](https://github.com/Leonidas-from-XIV/node-xml2js) | SOAP and XML responses; backs the legacy `xml2Json` global. |
| `postman-collection` | 5.3.1 | Apache-2.0 | [postmanlabs/postman-collection](https://github.com/postmanlabs/postman-collection) | `require('postman-collection')`, for scripts that construct SDK objects. |

Version choices that differ from Postman's own sandbox:

- **crypto-js 4.2.0**, not the 3.3.0 Postman ships. 3.x has CVE-2023-46233 (a
  weak PBKDF2 default). The only visible difference is that `PBKDF2` now
  defaults to SHA-256 with 250 000 iterations instead of SHA-1 with 1; a
  script that relies on the old defaults must pass them explicitly.
- **chai 4.4.1**, the last 4.x. chai 5 is ESM-only and would not load here.

`postman-collection` is the one heavy bundle (834 KiB, ~26 ms to evaluate), so
`pm.*` does not use it: `sandbox/` implements the `PropertyList`, `Url`,
`Request` and `Response` semantics directly against postman-collection's
source. The bundle is only paid for by a script that requires it by name.
