package diffview

// maxEditDistance is the largest edit distance (Myers D) we compute before
// giving up on a line-by-line diff.
const maxEditDistance = 5000

// shortestScript returns a shortest edit script using Myers O((N+M)*D) forward
// search with trace backtrack.
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
	trimmedA, trimmedB, pref, suf := trimEqual(a, b)
	a, b = trimmedA, trimmedB
	n, m := len(a), len(b)
	if n == 0 && m == 0 {
		return wrapOps(nil, pref, suf), true
	}
	if n == 0 {
		return wrapOps(repeatOps(opAdd, m), pref, suf), true
	}
	if m == 0 {
		return wrapOps(repeatOps(opDel, n), pref, suf), true
	}
	if n+m <= 512 {
		return wrapOps(trivialScript(a, b), pref, suf), true
	}

	maxK := n + m + 1
	offset := maxK
	size := 2*maxK + 1

	v := make([]int, size)
	for i := range v {
		v[i] = -1
	}
	// parent[d][k+offset] = 0 from k+1 (add), 1 from k-1 (del)
	var parents [][]int8

	for d := 0; d <= maxEditDistance; d++ {
		par := make([]int8, size)
		for k := -d; k <= d; k += 2 {
			idx := k + offset
			var x int
			if k == -d || (k != d && idx > 0 && idx+1 < size && v[idx-1] < v[idx+1]) {
				x = v[idx+1]
				par[idx] = 0
			} else if idx > 0 {
				x = v[idx-1] + 1
				par[idx] = 1
			} else {
				x = v[idx+1]
				par[idx] = 0
			}
			y := x - k
			for x < n && y >= 0 && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[idx] = x
			if x >= n && y >= m {
				mid := backtrackMyers(a, b, append(parents, par), d, k, offset)
				return wrapOps(mid, pref, suf), true
			}
		}
		parents = append(parents, par)
	}
	return nil, false
}

func backtrackMyers(a, b []string, parents [][]int8, d, k, offset int) []editOp {
	x, y := len(a), len(b)
	var ops []editOp
	for d > 0 {
		for x > 0 && y > 0 && a[x-1] == b[y-1] {
			x--
			y--
			ops = append(ops, opKeep)
		}
		idx := k + offset
		par := parents[d][idx]
		d--
		if par == 0 {
			k++
			y--
			ops = append(ops, opAdd)
		} else {
			k--
			x--
			ops = append(ops, opDel)
		}
	}
	for x > 0 && y > 0 && a[x-1] == b[y-1] {
		x--
		y--
		ops = append(ops, opKeep)
	}
	reverseOps(ops)
	return ops
}

func wrapOps(mid []editOp, pref, suf int) []editOp {
	out := make([]editOp, 0, pref+len(mid)+suf)
	for i := 0; i < pref; i++ {
		out = append(out, opKeep)
	}
	out = append(out, mid...)
	for i := 0; i < suf; i++ {
		out = append(out, opKeep)
	}
	return out
}

func repeatOps(op editOp, n int) []editOp {
	out := make([]editOp, n)
	for i := range out {
		out[i] = op
	}
	return out
}

func reverseOps(ops []editOp) {
	for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
		ops[i], ops[j] = ops[j], ops[i]
	}
}

func trivialScript(a, b []string) []editOp {
	var out []editOp
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			out = append(out, opKeep)
			i++
			j++
			continue
		}
		if len(a)-i > len(b)-j {
			out = append(out, opDel)
			i++
		} else {
			out = append(out, opAdd)
			j++
		}
	}
	for i < len(a) {
		out = append(out, opDel)
		i++
	}
	for j < len(b) {
		out = append(out, opAdd)
		j++
	}
	return out
}
