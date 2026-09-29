package script

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/require"

	"github.com/shubham/postctl/internal/script/assets"
)

// Bundled is the set of modules a script may require(). It matches Postman's
// sandbox, minus the ones postctl does not ship; see
// internal/script/assets/README.md for versions and licenses.
var Bundled = []string{
	"ajv", "chai", "chai-postman", "crypto-js", "lodash", "moment",
	"postman-collection", "tv4", "uuid", "xml2js",
}

var bundledSet = func() map[string]bool {
	m := make(map[string]bool, len(Bundled))
	for _, n := range Bundled {
		m[n] = true
	}
	return m
}()

// registry compiles and caches the bundles once for the whole process and is
// shared by every VM. It resolves nothing but the embedded assets: there is
// no path on which a script can reach the file system.
var registry = require.NewRegistry(require.WithLoader(loadBundle))

func loadBundle(p string) ([]byte, error) {
	name := strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(p, "\\", "/")), "/")
	name = strings.TrimPrefix(name, "node_modules/")
	name = strings.TrimSuffix(name, "/index.js")
	name = strings.TrimSuffix(name, ".js")
	if !bundledSet[name] {
		return nil, require.ModuleFileDoesNotExistError
	}
	return assets.Bundles.ReadFile("bundles/" + name + ".js")
}

var (
	bootOnce sync.Once
	bootProg *goja.Program
	bootErr  error
)

// bootProgram compiles the sandbox sources once. They are concatenated in
// file-name order and wrapped in one function so nothing they declare leaks
// into the global object that user scripts see.
func bootProgram() (*goja.Program, error) {
	bootOnce.Do(func() {
		entries, err := fs.ReadDir(assets.Sandbox, "sandbox")
		if err != nil {
			bootErr = fmt.Errorf("%w: %w", ErrNoRuntime, err)
			return
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".js") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		if len(names) == 0 {
			bootErr = fmt.Errorf("%w: no sandbox sources embedded", ErrNoRuntime)
			return
		}

		var b strings.Builder
		b.WriteString("(function(){\n")
		for _, n := range names {
			src, err := assets.Sandbox.ReadFile("sandbox/" + n)
			if err != nil {
				bootErr = fmt.Errorf("%w: %w", ErrNoRuntime, err)
				return
			}
			b.WriteString("/* ")
			b.WriteString(n)
			b.WriteString(" */\n")
			b.Write(src)
			b.WriteString("\n")
		}
		b.WriteString("\n})();")

		bootProg, err = goja.Compile("postctl-sandbox.js", b.String(), true)
		if err != nil {
			bootErr = fmt.Errorf("%w: %w", ErrNoRuntime, err)
		}
	})
	return bootProg, bootErr
}

// scriptPrefix wraps a script body so that each script in a chain gets its own
// scope (two scripts may both declare `const token`) and so that top-level
// await works, as it does in Postman. It deliberately adds no newline: line
// numbers in stack traces stay identical to the .js file, and only column
// numbers on the first line shift by len(scriptPrefix).
const scriptPrefix = "(async function(){"

const scriptSuffix = "\n})"

var (
	progMu    sync.Mutex
	progCache = map[string]*goja.Program{}
)

// progCacheMax bounds the compiled-script cache; a workspace with more
// distinct scripts than this simply recompiles the least recently seen ones.
const progCacheMax = 512

// userProgram compiles one script body, caching by content so a repeated send
// of the same request does not pay for parsing again.
func userProgram(name, source string) (*goja.Program, error) {
	sum := sha256.Sum256([]byte(name + "\x00" + source))
	key := hex.EncodeToString(sum[:])

	progMu.Lock()
	if p, ok := progCache[key]; ok {
		progMu.Unlock()
		return p, nil
	}
	progMu.Unlock()

	p, err := goja.Compile(name, scriptPrefix+source+scriptSuffix, false)
	if err != nil {
		return nil, err
	}

	progMu.Lock()
	if len(progCache) >= progCacheMax {
		progCache = map[string]*goja.Program{}
	}
	progCache[key] = p
	progMu.Unlock()

	return p, nil
}
