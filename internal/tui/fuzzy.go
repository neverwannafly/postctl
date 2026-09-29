package tui

import (
	"sort"
	"strings"
	"unicode"
)

// fuzzyMatch reports whether every rune of pattern appears in text in order
// (case-insensitively) and scores the best alignment. Higher is better.
// positions are rune indexes into text for highlighting.
//
// Scoring favours consecutive runs, matches at word starts, and short texts,
// so "cc" ranks "Create charge" above "Cancel subscription plan".
func fuzzyMatch(pattern, text string) (score int, positions []int, ok bool) {
	p := []rune(strings.ToLower(pattern))
	if len(p) == 0 {
		return 0, nil, true
	}
	orig := []rune(text)
	t := []rune(strings.ToLower(text))
	if len(t) < len(p) {
		return 0, nil, false
	}

	// Forward pass finds the earliest end of a full match; a backward pass
	// then tightens the start so the window is as short as possible.
	pi := 0
	end := -1
	for i := 0; i < len(t) && pi < len(p); i++ {
		if t[i] == p[pi] {
			pi++
			if pi == len(p) {
				end = i
			}
		}
	}
	if pi < len(p) {
		return 0, nil, false
	}
	pos := make([]int, len(p))
	pi = len(p) - 1
	for i := end; i >= 0 && pi >= 0; i-- {
		if t[i] == p[pi] {
			pos[pi] = i
			pi--
		}
	}

	score = 100 - len(orig) // shorter is better
	prev := -2
	for k, i := range pos {
		bonus := 1
		if i == 0 || isBoundary(orig[i-1]) {
			bonus += 8 // word start
		} else if unicode.IsLower(orig[i-1]) && unicode.IsUpper(orig[i]) {
			bonus += 6 // camelCase hump
		}
		if i == prev+1 {
			bonus += 10 // consecutive run
		}
		if k == 0 && i == 0 {
			bonus += 6 // anchored at the start
		}
		score += bonus
		prev = i
	}
	score -= (pos[len(pos)-1] - pos[0] + 1 - len(pos)) * 2 // gaps inside the window
	return score, pos, true
}

func isBoundary(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune("/_-.:›>{}[]()", r)
}

// ranked is a scored candidate index.
type ranked struct {
	index int
	score int
	pos   []int
}

// rankBy scores each candidate label against query and returns the matches
// best first. Ties keep the input order, so sources can pre-order by
// importance. An empty query matches everything in input order.
func rankBy(query string, n int, label func(i int) string, limit int) []ranked {
	out := make([]ranked, 0, min(n, 64))
	for i := 0; i < n; i++ {
		s, pos, ok := fuzzyMatch(query, label(i))
		if !ok {
			continue
		}
		out = append(out, ranked{index: i, score: s, pos: pos})
	}
	if strings.TrimSpace(query) != "" {
		sort.SliceStable(out, func(a, b int) bool { return out[a].score > out[b].score })
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
