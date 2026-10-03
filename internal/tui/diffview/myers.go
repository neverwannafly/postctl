package diffview

// shortestScript returns a shortest edit script using Hirschberg's algorithm
// (Myers/LCS) in linear space.
func shortestScript(a, b []string) []editOp {
	return lcsScript(a, b, 0, len(a), 0, len(b))
}

func lcsScript(a, b []string, aLo, aHi, bLo, bHi int) []editOp {
	an := aHi - aLo
	bn := bHi - bLo
	if an == 0 {
		out := make([]editOp, bn)
		for i := range out {
			out[i] = opAdd
		}
		return out
	}
	if bn == 0 {
		out := make([]editOp, an)
		for i := range out {
			out[i] = opDel
		}
		return out
	}
	if an == 1 || bn == 1 {
		return trivialScript(a[aLo:aHi], b[bLo:bHi])
	}
	mid := aLo + an/2
	l1 := lcsLengths(a, b, aLo, mid, bLo, bHi)
	l2 := lcsLengthsReverse(a, b, mid, aHi, bLo, bHi)
	bestJ := bLo
	best := -1
	for j := bLo; j <= bHi; j++ {
		sum := l1[j-bLo] + l2[j-bLo]
		if sum > best {
			best = sum
			bestJ = j
		}
	}
	left := lcsScript(a, b, aLo, mid, bLo, bestJ)
	right := lcsScript(a, b, mid, aHi, bestJ, bHi)
	return append(left, right...)
}

// lcsLengths[out[j-bLo]] = LCS(a[aLo:aHi], b[bLo:bLo+j]) with out[0]=0.
func lcsLengths(a, b []string, aLo, aHi, bLo, bHi int) []int {
	bn := bHi - bLo
	prev := make([]int, bn+1)
	curr := make([]int, bn+1)
	for i := aLo; i < aHi; i++ {
		curr[0] = 0
		for j := bLo; j < bHi; j++ {
			jj := j - bLo + 1
			if a[i] == b[j] {
				curr[jj] = prev[jj-1] + 1
			} else if prev[jj] >= curr[jj-1] {
				curr[jj] = prev[jj]
			} else {
				curr[jj] = curr[jj-1]
			}
		}
		prev, curr = curr, prev
	}
	out := make([]int, bn+1)
	copy(out, prev)
	return out
}

// lcsLengthsReverse[out[j-bLo]] = LCS(a[mid:aHi], b[j:bHi]).
func lcsLengthsReverse(a, b []string, aLo, aHi, bLo, bHi int) []int {
	bn := bHi - bLo
	prev := make([]int, bn+1)
	curr := make([]int, bn+1)
	for i := aHi - 1; i >= aLo; i-- {
		curr[bn] = 0
		for j := bHi - 1; j >= bLo; j-- {
			jj := j - bLo
			if a[i] == b[j] {
				curr[jj] = prev[jj+1] + 1
			} else if prev[jj] >= curr[jj+1] {
				curr[jj] = prev[jj]
			} else {
				curr[jj] = curr[jj+1]
			}
		}
		prev, curr = curr, prev
	}
	out := make([]int, bn+1)
	copy(out, prev)
	return out
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
