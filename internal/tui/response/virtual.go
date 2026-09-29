package response

import (
	"bytes"
)

// lineIndex maps line numbers to byte offsets in body (for virtualization).
type lineIndex struct {
	offsets []int
}

func (idx *lineIndex) reset() {
	idx.offsets = idx.offsets[:0]
}

func (idx *lineIndex) build(body []byte) {
	idx.reset()
	idx.offsets = append(idx.offsets, 0)
	for i, b := range body {
		if b == '\n' {
			idx.offsets = append(idx.offsets, i+1)
		}
	}
}

func (idx *lineIndex) lineCount() int {
	if len(idx.offsets) == 0 {
		return 0
	}
	return len(idx.offsets)
}

func (idx *lineIndex) lineAt(body []byte, n int) string {
	if n < 0 || n >= len(idx.offsets) {
		return ""
	}
	start := idx.offsets[n]
	end := len(body)
	if n+1 < len(idx.offsets) {
		end = idx.offsets[n+1]
	}
	line := body[start:end]
	return string(bytes.TrimSuffix(line, []byte{'\r'}))
}
