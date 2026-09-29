package store

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Fixed file and directory names.
const (
	workspaceYAML   = "postctl.yaml"
	globalsYAML     = "globals.yaml"
	collectionsDir  = "collections"
	environmentsDir = "environments"
	stateDir        = ".postctl"
	collectionYAML  = "collection.yaml"
	folderYAML      = "folder.yaml"
	preScript       = "pre.js"
	testScript      = "test.js"
	preSuffix       = ".pre.js"
	testSuffix      = ".test.js"
	maxSlugLen      = 60
)

// Slug converts a display name to a filesystem-safe lower-case ASCII slug:
// letters and digits are kept, every other run of characters becomes a single
// "-", and the result is trimmed and capped at 60 bytes. Names with no usable
// characters yield fallback.
func Slug(name, fallback string) string {
	var b strings.Builder
	dash := true // suppress leading dash
	for _, r := range strings.ToLower(name) {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
			dash = false
		default:
			if !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > maxSlugLen {
		s = strings.Trim(s[:maxSlugLen], "-")
	}
	if s == "" {
		return fallback
	}
	return s
}

// uniqueSlug returns slug, or slug-2, slug-3, ... whichever is not in taken,
// and records the result in taken.
func uniqueSlug(slug string, taken map[string]bool) string {
	out := slug
	for n := 2; taken[out]; n++ {
		out = slug + "-" + strconv.Itoa(n)
	}
	taken[out] = true
	return out
}

// numPrefix parses a leading run of digits followed by '-' or '_'.
func numPrefix(name string) (int, bool) {
	i := 0
	for i < len(name) && name[i] >= '0' && name[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(name) || (name[i] != '-' && name[i] != '_') {
		return 0, false
	}
	n, err := strconv.Atoi(name[:i])
	return n, err == nil
}

// orderEntries sorts names by the explicit order list first, then by numeric
// prefix, then alphabetically.
func orderEntries(names []string, order []string) []string {
	rank := make(map[string]int, len(order))
	for i, n := range order {
		if _, dup := rank[n]; !dup {
			rank[n] = i
		}
	}
	out := append([]string(nil), names...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		ra, aok := rank[a]
		rb, bok := rank[b]
		switch {
		case aok && bok:
			return ra < rb
		case aok != bok:
			return aok
		}
		na, apre := numPrefix(a)
		nb, bpre := numPrefix(b)
		switch {
		case apre && bpre && na != nb:
			return na < nb
		case apre != bpre:
			return apre
		}
		return a < b
	})
	return out
}

// prefixWidth is the zero-padded width of request order prefixes for n
// siblings (at least 3).
func prefixWidth(n int) int {
	w := len(strconv.Itoa(n))
	if w < 3 {
		return 3
	}
	return w
}

func requestStem(index, count int, name string) string {
	return fmt.Sprintf("%0*d-%s", prefixWidth(count), index+1, Slug(name, "request"))
}
