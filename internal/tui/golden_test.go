package tui

import (
	"fmt"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

type goldenCase struct {
	name string
	w, h int
}

var goldenSizes = []goldenCase{{"80x24", 80, 24}, {"120x40", 120, 40}, {"200x60", 200, 60}}

// TestGoldenScreens pins the plain-text look of the main states at the three
// reference sizes. Regenerate with: go test ./internal/tui -update
func TestGoldenScreens(t *testing.T) {
	type scene struct {
		name  string
		build func(t *testing.T, w, h int) *harness
	}
	scenes := []scene{
		{"tree", func(t *testing.T, w, h int) *harness {
			hh := newHarness(t, demo(), w, h)
			hh.selectByName("Create charge")
			return hh
		}},
		{"request-focused", func(t *testing.T, w, h int) *harness {
			hh := newHarness(t, demo(), w, h)
			hh.selectByName("Refund charge")
			hh.keys("enter")
			return hh
		}},
		{"env-switcher", func(t *testing.T, w, h int) *harness {
			hh := newHarness(t, demo(), w, h)
			hh.keys("E", "down", "down", "down")
			return hh
		}},
		{"palette", func(t *testing.T, w, h int) *harness {
			hh := newHarness(t, demo(), w, h)
			hh.keys("ctrl+p")
			hh.typeText("cust")
			return hh
		}},
		{"help", func(t *testing.T, w, h int) *harness {
			hh := newHarness(t, demo(), w, h)
			hh.keys("?")
			return hh
		}},
		{"delete-dialog", func(t *testing.T, w, h int) *harness {
			hh := newHarness(t, demo(), w, h)
			hh.selectByName("Customers")
			hh.keys("d")
			return hh
		}},
		{"filter", func(t *testing.T, w, h int) *harness {
			hh := newHarness(t, demo(), w, h)
			hh.keys("/")
			hh.typeText("charge")
			return hh
		}},
		{"first-run", func(t *testing.T, w, h int) *harness {
			return newHarness(t, nil, w, h)
		}},
		{"empty-workspace", func(t *testing.T, w, h int) *harness {
			return newHarness(t, &model.Workspace{ID: "w", Name: "empty"}, w, h)
		}},
		{"problems", func(t *testing.T, w, h int) *harness {
			be := newFake(demo())
			be.problems = append(be.problems, brokenFile(be.root, "collections/payments/charges/create.yaml", "yaml: line 3: mapping values are not allowed here"))
			return newHarnessWith(t, be, w, h)
		}},
		{"load-failed", func(t *testing.T, w, h int) *harness {
			be := newFake(demo())
			be.loadErr = fmt.Errorf("permission denied reading %s", be.root)
			return newHarnessWith(t, be, w, h)
		}},
	}
	for _, sc := range scenes {
		for _, sz := range goldenSizes {
			t.Run(sc.name+"/"+sz.name, func(t *testing.T) {
				hh := sc.build(t, sz.w, sz.h)
				golden(t, sc.name+"-"+sz.name, hh.screen())
			})
		}
	}
}

// TestGoldenModes pins the look under NO_COLOR, --ascii, --accessible and
// the two themes (with colors) at the smallest and largest sizes.
func TestGoldenModes(t *testing.T) {
	modes := []struct {
		name string
		opt  harnessOpt
		raw  bool
	}{
		{"nocolor", func(o *Options) { o.NoColor = true }, false},
		{"ascii", func(o *Options) { o.ASCII = true }, false},
		{"accessible", func(o *Options) { o.Accessible = true }, false},
		{"dark", func(o *Options) {}, true},
		{"light", func(o *Options) { o.Light = true }, true},
	}
	for _, m := range modes {
		for _, sz := range []goldenCase{goldenSizes[0], goldenSizes[2]} {
			t.Run(m.name+"/"+sz.name, func(t *testing.T) {
				hh := newHarness(t, demo(), sz.w, sz.h, m.opt)
				hh.selectByName("Create charge")
				out := hh.screen()
				if m.raw {
					out = hh.rawScreen()
				}
				golden(t, "mode-"+m.name+"-"+sz.name, out)
			})
		}
	}
}
