package script

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/vars"
)

// The user's own collections, extracted from Postman into testdata/private,
// are the reason this package exists: their scripts have to run unmodified.
// The directory is gitignored, so this test skips wherever it is absent.
//
// Nothing from that data may be printed. Request names and counts are fine;
// script text, variable values, header values, bodies and URLs are not, so
// failures report a request name and an error name and nothing more. Pass
// -private-detail when you are looking at the data locally anyway and need
// the message too.
const privateDir = "../../testdata/private/staging"

var privateDetail = flag.Bool("private-detail", false,
	"include error messages from the private collections in the failure output")

// v21Collection is the slice of the Postman v2.1 schema this test reads.
type v21Collection struct {
	Info     struct{ Name string } `json:"info"`
	Item     []v21Item             `json:"item"`
	Event    []v21Event            `json:"event"`
	Variable []v21Variable         `json:"variable"`
}

type v21Item struct {
	Name    string          `json:"name"`
	Item    []v21Item       `json:"item"`
	Event   []v21Event      `json:"event"`
	Request json.RawMessage `json:"request"`
}

type v21Event struct {
	Listen string `json:"listen"`
	Script struct {
		// exec is a string in older exports and an array of lines in newer
		// ones.
		Exec json.RawMessage `json:"exec"`
	} `json:"script"`
}

type v21Variable struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

type v21Environment struct {
	Name   string        `json:"name"`
	Values []v21EnvValue `json:"values"`
}

type v21EnvValue struct {
	Key     string `json:"key"`
	Value   any    `json:"value"`
	Enabled *bool  `json:"enabled"`
}

func (e v21Event) source() string {
	if len(e.Script.Exec) == 0 {
		return ""
	}
	var lines []string
	if err := json.Unmarshal(e.Script.Exec, &lines); err == nil {
		return strings.Join(lines, "\n")
	}
	var one string
	if err := json.Unmarshal(e.Script.Exec, &one); err == nil {
		return one
	}
	return ""
}

// privateScript is one event script with the path that identifies it.
type privateScript struct {
	path   string // "Collection/Folder/Request"
	event  Event
	source string
}

// privateChain is everything that runs for one request and one event, in
// Postman's order: collection, then each enclosing folder, then the request.
type privateChain struct {
	request string
	event   Event
	scripts []privateScript
}

func TestPrivateCollections(t *testing.T) {
	dir := filepath.Join(privateDir, "collections")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("no extracted collections at %s", privateDir)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Skipf("no collections in %s", dir)
	}

	store := privateStore(t)

	var (
		chains  []privateChain
		scripts = map[string]bool{} // path -> ran clean
	)
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var c v21Collection
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatalf("%s: %v", filepath.Base(f), err)
		}
		name := c.Info.Name
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(f), ".json")
		}
		for _, v := range c.Variable {
			store.Set(vars.Collection, v.Key, v.Value)
		}
		chains = append(chains, collectChains(name, name, c.Item, eventsOf(c.Event, name))...)
	}

	var (
		failures []string
		unsup    []string
		sends    int
		writes   int
	)

	for _, chain := range chains {
		run := make([]*model.Script, 0, len(chain.scripts))
		for _, s := range chain.scripts {
			scripts[s.path] = scripts[s.path] || false
			run = append(run, &model.Script{File: s.path, Source: s.source})
		}

		in := Input{
			Event:   chain.event,
			Request: model.NewRequest(chain.request, "GET", "https://example.invalid/private"),
			Vars:    store,
			Sender:  &fakeSender{},
			Cookies: newJar(),
			Info: Info{
				RequestName: lastSegment(chain.request), RequestID: "private",
				CollectionName: firstSegment(chain.request), IterationCount: 1,
			},
		}
		if chain.event == EventTest {
			in.Response = jsonResponse(200, `{"ok":true}`)
		}

		sender := in.Sender.(*fakeSender)

		res, err := Run(context.Background(), run, in)
		if err != nil {
			t.Fatalf("%s: %v", chain.request, err)
		}
		sends += sender.count()
		for _, changes := range store.Changes() {
			writes += len(changes)
		}
		store.changes = map[vars.Scope][]vars.Change{}

		// A no-op API warns instead of throwing, so it would otherwise pass
		// unnoticed. Only the notice is recorded, never what the script logged.
		for _, entry := range res.Console {
			if entry.Level == LevelWarn && strings.Contains(entry.Message, "not supported by postctl") {
				unsup = append(unsup, chain.request+": "+entry.Message)
			}
		}

		if res.Err == nil {
			for _, s := range chain.scripts {
				scripts[s.path] = true
			}
			continue
		}
		detail := ""
		if *privateDetail {
			detail = ": " + res.Err.Message
		}
		failures = append(failures, fmt.Sprintf("%s (%s, line %d)%s",
			res.Err.File, res.Err.Name, res.Err.Line, detail))
	}

	clean := 0
	for _, ok := range scripts {
		if ok {
			clean++
		}
	}

	t.Logf("%d chains over %d scripts in the extracted collections: %d scripts ran clean, %d chains errored",
		len(chains), len(scripts), clean, len(failures))
	t.Logf("the scripts made %d variable writes and %d pm.sendRequest calls, and hit %d no-op APIs",
		writes, sends, len(unsup))

	for _, f := range failures {
		t.Errorf("script did not run: %s", f)
	}
	for _, u := range unsup {
		t.Errorf("script reached an unsupported API: %s", u)
	}
}

// collectChains walks the item tree, carrying the scripts inherited from the
// collection and the folders above.
func collectChains(collection, path string, items []v21Item, inherited map[Event][]privateScript) []privateChain {
	var out []privateChain

	for _, it := range items {
		here := path + "/" + it.Name
		own := eventsOf(it.Event, here)

		if len(it.Item) > 0 {
			merged := map[Event][]privateScript{}
			for _, ev := range []Event{EventPreRequest, EventTest} {
				merged[ev] = append(append([]privateScript{}, inherited[ev]...), own[ev]...)
			}
			out = append(out, collectChains(collection, here, it.Item, merged)...)
			continue
		}

		for _, ev := range []Event{EventPreRequest, EventTest} {
			chain := append(append([]privateScript{}, inherited[ev]...), own[ev]...)
			if len(chain) == 0 {
				continue
			}
			out = append(out, privateChain{request: here, event: ev, scripts: chain})
		}
	}
	return out
}

func eventsOf(events []v21Event, path string) map[Event][]privateScript {
	out := map[Event][]privateScript{}
	for _, e := range events {
		var ev Event
		switch e.Listen {
		case "prerequest":
			ev = EventPreRequest
		case "test":
			ev = EventTest
		default:
			continue
		}
		source := e.source()
		if strings.TrimSpace(source) == "" {
			continue
		}
		out[ev] = append(out[ev], privateScript{path: path + " [" + e.Listen + "]", event: ev, source: source})
	}
	return out
}

// privateStore seeds every extracted environment into one store, so scripts
// that read their own variables get plausible values rather than undefined.
func privateStore(t *testing.T) *fakeStore {
	t.Helper()

	store := newStore()
	files, err := filepath.Glob(filepath.Join(privateDir, "environments", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var env v21Environment
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("%s: %v", filepath.Base(f), err)
		}
		for _, v := range env.Values {
			if v.Enabled != nil && !*v.Enabled {
				continue
			}
			store.Set(vars.Environment, v.Key, v.Value)
		}
	}
	return store
}

func firstSegment(path string) string {
	if i := strings.IndexByte(path, '/'); i >= 0 {
		return path[:i]
	}
	return path
}

func lastSegment(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}
