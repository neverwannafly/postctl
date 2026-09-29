// Package assets embeds the pinned JavaScript that the postctl script sandbox
// loads into goja: the third-party libraries reachable through require() and
// postctl's own sandbox bootstrap code.
//
// Nothing here is fetched or built at run time. See README.md for versions,
// sources and licenses, and build.js (task assets) for how to rebuild.
package assets

import "embed"

// Bundles holds the minified UMD builds of the third-party libraries, keyed by
// "<module>.js" under bundles/.
//
//go:embed bundles/*.js
var Bundles embed.FS

// Sandbox holds postctl's own sandbox JavaScript under sandbox/.
//
//go:embed sandbox/*.js
var Sandbox embed.FS
