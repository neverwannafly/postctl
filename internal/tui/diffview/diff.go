// Package diffview renders a unified line diff of redacted text for the TUI.
package diffview

import (
	"strings"
)

// LineKind classifies one line in a unified diff.
type LineKind int

// Line kinds for unified diff output.
const (
	LineSame LineKind = iota
	LineAdd
	LineRemove
)

// Line is one row of a unified diff.
type Line struct {
	// Kind is same, add or remove.
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

// Diff computes a line-based diff between a and b using a Myers-style
// shortest-edit script on rune slices split by newline boundaries.
func Diff(a, b string) []Line {
	aLines := splitLines(a)
	bLines := splitLines(b)
	script := shortestScript(aLines, bLines)
	return buildUnified(aLines, bLines, script)
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

func shortestScript(a, b []string) []editOp {
	n, m := len(a), len(b)
	// DP table for LCS backtracking; cap dimensions by line count (already capped upstream).
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var script []editOp
	i, j := 0, 0
	for i < n && j < m {
		if a[i] == b[j] {
			script = append(script, opKeep)
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			script = append(script, opDel)
			i++
		} else {
			script = append(script, opAdd)
			j++
		}
	}
	for i < n {
		script = append(script, opDel)
		i++
	}
	for j < m {
		script = append(script, opAdd)
		j++
	}
	return script
}

func buildUnified(a, b []string, script []editOp) []Line {
	var out []Line
	ai, bi := 0, 0
	for _, op := range script {
		switch op {
		case opKeep:
			out = append(out, Line{Kind: LineSame, Text: a[ai]})
			ai++
			bi++
		case opDel:
			out = append(out, Line{Kind: LineRemove, Text: a[ai]})
			ai++
		case opAdd:
			out = append(out, Line{Kind: LineAdd, Text: b[bi]})
			bi++
		}
	}
	return out
}

// Hunks finds change regions in unified diff lines.
func Hunks(lines []Line) []Hunk {
	var hunks []Hunk
	start := -1
	for i, ln := range lines {
		if ln.Kind == LineSame {
			if start >= 0 {
				hunks = append(hunks, Hunk{Start: start, End: i})
				start = -1
			}
			continue
		}
		if start < 0 {
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
