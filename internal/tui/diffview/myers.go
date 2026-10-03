package diffview

// maxEditDistance is the largest edit distance (Myers D) we compute before
// giving up on a line-by-line diff.
const maxEditDistance = 5000

// shortestScript returns a shortest edit script using the linear-space
// middle-snake divide and conquer from Myers (1986), section 4b.
func shortestScript(a, b []string) []editOp {
	script, ok := myersScript(a, b)
	if !ok {
		return nil
	}
	return script
}

func trimEqual(a, b []string) (trimmedA, trimmedB []string, prefix, suffix int) {
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	return a[p : len(a)-s], b[p : len(b)-s], p, s
}

func myersScript(a, b []string) ([]editOp, bool) {
	out := make([]editOp, 0, len(a)+len(b))
	edits := 0
	if !appendMyers(&out, a, b, &edits) {
		return nil, false
	}
	return out, true
}

func appendMyers(out *[]editOp, a, b []string, edits *int) bool {
	a, b, pref, suf := trimEqual(a, b)
	*out = append(*out, repeatOps(opKeep, pref)...)
	if len(a) == 0 {
		if !appendEdits(out, opAdd, len(b), edits) {
			return false
		}
	} else if len(b) == 0 {
		if !appendEdits(out, opDel, len(a), edits) {
			return false
		}
	} else {
		x, y, ok := middleSplit(a, b)
		if !ok {
			return false
		}
		if (x == 0 && y == 0) || (x == len(a) && y == len(b)) {
			if !appendEdits(out, opDel, len(a), edits) ||
				!appendEdits(out, opAdd, len(b), edits) {
				return false
			}
		} else if !appendMyers(out, a[:x], b[:y], edits) ||
			!appendMyers(out, a[x:], b[y:], edits) {
			return false
		}
	}
	*out = append(*out, repeatOps(opKeep, suf)...)
	return true
}

func appendEdits(out *[]editOp, op editOp, n int, edits *int) bool {
	if *edits+n > maxEditDistance {
		return false
	}
	*edits += n
	*out = append(*out, repeatOps(op, n)...)
	return true
}

// middleSplit finds where the forward and reverse edit frontiers overlap.
// Each frontier uses one diagonal vector; no per-D trace is retained.
func middleSplit(a, b []string) (int, int, bool) {
	n, m := len(a), len(b)
	maxD := (n + m + 1) / 2
	searchD := min(maxD, maxEditDistance/2+1)
	offset := maxD + 1
	size := 2*maxD + 3
	forward := make([]int, size)
	reverse := make([]int, size)
	for i := range forward {
		forward[i], reverse[i] = -1, -1
	}
	forward[offset+1] = 0
	reverse[offset+1] = 0
	delta := n - m
	odd := delta&1 != 0
	fStart, fEnd, rStart, rEnd := 0, 0, 0, 0

	for d := 0; d <= searchD; d++ {
		for k := -d + fStart; k <= d-fEnd; k += 2 {
			idx := offset + k
			x := 0
			if k == -d || (k != d && forward[idx-1] < forward[idx+1]) {
				x = forward[idx+1]
			} else {
				x = forward[idx-1] + 1
			}
			y := x - k
			for x < n && y >= 0 && y < m && a[x] == b[y] {
				x++
				y++
			}
			forward[idx] = x
			switch {
			case x > n:
				fEnd += 2
			case y > m:
				fStart += 2
			case odd:
				rk := delta - k
				ridx := offset + rk
				if ridx >= 0 && ridx < size && reverse[ridx] != -1 && x >= n-reverse[ridx] {
					return x, y, true
				}
			}
		}

		for k := -d + rStart; k <= d-rEnd; k += 2 {
			idx := offset + k
			x := 0
			if k == -d || (k != d && reverse[idx-1] < reverse[idx+1]) {
				x = reverse[idx+1]
			} else {
				x = reverse[idx-1] + 1
			}
			y := x - k
			for x < n && y >= 0 && y < m && a[n-x-1] == b[m-y-1] {
				x++
				y++
			}
			reverse[idx] = x
			switch {
			case x > n:
				rEnd += 2
			case y > m:
				rStart += 2
			case !odd:
				fk := delta - k
				fidx := offset + fk
				if fidx >= 0 && fidx < size && forward[fidx] != -1 {
					fx := forward[fidx]
					fy := fx - fk
					if fx >= n-x {
						return fx, fy, true
					}
				}
			}
		}
	}
	return 0, 0, false
}

func repeatOps(op editOp, n int) []editOp {
	out := make([]editOp, n)
	for i := range out {
		out[i] = op
	}
	return out
}
