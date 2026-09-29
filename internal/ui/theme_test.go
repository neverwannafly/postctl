package ui

import (
	"math"
	"strconv"
	"testing"
)

func TestEveryTokenResolves(t *testing.T) {
	for _, tok := range Tokens() {
		for _, dark := range []bool{true, false} {
			if Hex(tok, dark) == "" {
				t.Errorf("token %s has no value (dark=%v)", tok, dark)
			}
			th := NewTheme(dark, false, false)
			if th.Color(tok) == nil {
				t.Errorf("token %s resolves to nil color", tok)
			}
		}
	}
	if len(Tokens()) != 10+16 {
		t.Errorf("token count %d: update docs/design-language.md if tokens changed", len(Tokens()))
	}
}

func TestNoColorHasNoColors(t *testing.T) {
	th := NewTheme(true, true, false)
	for _, tok := range Tokens() {
		if th.Color(tok) != nil {
			t.Errorf("%s returned a color in NoColor mode", tok)
		}
	}
	if th.SelectedBG() != nil || th.BorderColor() != nil {
		t.Error("backgrounds must be nil in NoColor mode")
	}
	if !th.Style(StatusDanger).GetBold() {
		t.Error("status tokens must stay bold without color")
	}
}

func TestMethodAndStatusMapping(t *testing.T) {
	for m, want := range map[string]Token{
		"get": MethodGet, "POST": MethodPost, "Put": MethodPut, "PATCH": MethodPatch,
		"DELETE": MethodDelete, "HEAD": MethodOther, "PURGE": MethodOther, "": MethodOther,
	} {
		if got := MethodToken(m); got != want {
			t.Errorf("MethodToken(%q) = %s, want %s", m, got, want)
		}
	}
	for code, want := range map[int]Token{
		200: Status2xx, 204: Status2xx, 301: Status3xx, 404: Status4xx, 503: Status5xx, 0: Status5xx, 101: StatusInfo,
	} {
		if got := StatusToken(code); got != want {
			t.Errorf("StatusToken(%d) = %s, want %s", code, got, want)
		}
	}
	if Hex(MethodGet, true) != Hex(StatusInfo, true) || Hex(MethodDelete, false) != Hex(StatusDanger, false) {
		t.Error("method colors must alias status colors")
	}
}

func TestLayoutBreakpoints(t *testing.T) {
	for w, want := range map[int]Layout{
		200: LayoutThreePane, 140: LayoutThreePane, 139: LayoutTwoPane, 100: LayoutTwoPane, 99: LayoutSingle, 80: LayoutSingle, 0: LayoutSingle,
	} {
		if got := LayoutFor(w); got != want {
			t.Errorf("LayoutFor(%d) = %v, want %v", w, got, want)
		}
	}
}

func luminance(hex string) float64 {
	v, _ := strconv.ParseUint(hex[1:], 16, 32)
	ch := func(c uint64) float64 {
		f := float64(c) / 255
		if f <= 0.03928 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(v>>16&0xff) + 0.7152*ch(v>>8&0xff) + 0.0722*ch(v&0xff)
}

func contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// The design language claims body and status colors reach WCAG 2.2 4.5:1 on
// the representative backgrounds; keep that honest.
func TestPaletteContrast(t *testing.T) {
	const darkBG, lightBG = "#101820", "#F7F9FB"
	for _, tok := range []Token{TextPrimary, TextSecondary, TextQuiet, AccentPrimary, StatusSuccess, StatusWarning, StatusDanger, StatusInfo} {
		if c := contrast(Hex(tok, true), darkBG); c < 4.5 {
			t.Errorf("%s dark contrast %.2f < 4.5", tok, c)
		}
		if c := contrast(Hex(tok, false), lightBG); c < 4.5 {
			t.Errorf("%s light contrast %.2f < 4.5", tok, c)
		}
	}
}

func TestGlyphSets(t *testing.T) {
	u, a := GlyphSet(false), GlyphSet(true)
	if u.Select == a.Select || a.Ellipsis != "..." || a.Expanded == "" || u.Collapsed == "" {
		t.Errorf("unexpected glyph sets: %+v %+v", u, a)
	}
}
