// Package diffview renders a unified line diff of redacted text for the TUI.
package diffview

import (
	"fmt"
	"strings"
)

// LineKind classifies one line in a unified diff.
type LineKind int

// Line kinds for unified diff output.
const (
	LineSame LineKind = iota
	LineAdd
	LineRemove
	// LineHunkHeader is a unified diff hunk header (@@ ... @@).
	LineHunkHeader
)

// Line is one row of a unified diff.
type Line struct {
	// Kind is same, add, remove or hunk header.
	Kind LineKind
	// Text is the line content without a diff prefix.
	Text string
}

// Hunk groups contiguous changed lines with a header index.
type Hunk struct {
	// Start is the index in Lines of the hunk header (@@) row.
	Start int
	// End is the index after the last line belonging to this hunk.
	End int
}

// DefaultMaxLines is the per-side line cap before truncation.
const DefaultMaxLines = 20000

const contextLines = 3

// DiffTooBig reports that edit distance exceeded the cap.
type DiffTooBig struct {
	LeftLines  int
	RightLines int
}

func (e DiffTooBig) Error() string {
	return fmt.Sprintf("Responses differ too much to diff line by line (%d vs %d lines)", e.LeftLines, e.RightLines)
}

// Diff computes a line-based unified diff between a and b.
func Diff(a, b string) []Line {
	lines, _, _ := Compute(a, b)
	return lines
}

// Compute builds a unified diff or reports truncation or DiffTooBig.
func Compute(a, b string) (lines []Line, truncated bool, err error) {
	left, trL := Cap(a, DefaultMaxLines)
	right, trR := Cap(b, DefaultMaxLines)
	truncated = trL || trR
	aLines := splitLines(left)
	bLines := splitLines(right)
	script := shortestScript(aLines, bLines)
	if script == nil {
		return nil, truncated, DiffTooBig{LeftLines: len(aLines), RightLines: len(bLines)}
	}
	return buildUnified(aLines, bLines, script), truncated, nil
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\n")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

type editOp int

const (
	opKeep editOp = iota
	opDel
	opAdd
)

func buildUnified(a, b []string, script []editOp) []Line {
	type row struct {
		kind LineKind
		text string
	}
	var raw []row
	ai, bi := 0, 0
	for _, op := range script {
		switch op {
		case opKeep:
			raw = append(raw, row{LineSame, a[ai]})
			ai++
			bi++
		case opDel:
			raw = append(raw, row{LineRemove, a[ai]})
			ai++
		case opAdd:
			raw = append(raw, row{LineAdd, b[bi]})
			bi++
		}
	}
	if len(raw) == 0 {
		return nil
	}
	// Collect change indices.
	var changes []int
	for i, r := range raw {
		if r.kind != LineSame {
			changes = append(changes, i)
		}
	}
	if len(changes) == 0 {
		out := make([]Line, len(raw))
		for i, r := range raw {
			out[i] = Line{Kind: r.kind, Text: r.text}
		}
		return out
	}
	// Merge change regions with context.
	type region struct{ lo, hi int }
	var regions []region
	cl := max(0, changes[0]-contextLines)
	ch := min(len(raw)-1, changes[0]+contextLines)
	for _, c := range changes[1:] {
		lo := max(0, c-contextLines)
		hi := min(len(raw)-1, c+contextLines)
		if lo <= ch+1 {
			ch = hi
		} else {
			regions = append(regions, region{cl, ch})
			cl, ch = lo, hi
		}
	}
	regions = append(regions, region{cl, ch})

	var out []Line
	oldLine, newLine := 1, 1
	prevHi := -1
	for _, reg := range regions {
		for i := prevHi + 1; i < reg.lo; i++ {
			if raw[i].kind == LineSame {
				oldLine++
				newLine++
			}
		}
		oldStart, newStart := oldLine, newLine
		oldCount, newCount := 0, 0
		for i := reg.lo; i <= reg.hi; i++ {
			switch raw[i].kind {
			case LineSame:
				oldCount++
				newCount++
			case LineRemove:
				oldCount++
			case LineAdd:
				newCount++
			}
		}
		hdr := fmt.Sprintf("@@ -%d,%d +%d,%d @@", oldStart, oldCount, newStart, newCount)
		out = append(out, Line{Kind: LineHunkHeader, Text: hdr})
		for i := reg.lo; i <= reg.hi; i++ {
			out = append(out, Line{Kind: raw[i].kind, Text: raw[i].text})
			switch raw[i].kind {
			case LineSame:
				oldLine++
				newLine++
			case LineRemove:
				oldLine++
			case LineAdd:
				newLine++
			}
		}
		prevHi = reg.hi
	}
	return out
}

// Hunks finds change regions in unified diff lines.
func Hunks(lines []Line) []Hunk {
	var hunks []Hunk
	start := -1
	for i, ln := range lines {
		if ln.Kind == LineHunkHeader {
			if start >= 0 {
				hunks = append(hunks, Hunk{Start: start, End: i})
			}
			start = i
		}
	}
	if start >= 0 {
		hunks = append(hunks, Hunk{Start: start, End: len(lines)})
	}
	return hunks
}

// Cap truncates side text to max lines and reports truncation.
func Cap(text string, max int) (string, bool) {
	if max <= 0 {
		max = DefaultMaxLines
	}
	lines := splitLines(text)
	if len(lines) <= max {
		return text, false
	}
	return strings.Join(lines[:max], "\n"), true
}
