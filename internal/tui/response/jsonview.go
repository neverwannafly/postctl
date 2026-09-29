package response

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
)

var jsonPathIndex = regexp.MustCompile(`\[(\d+)\]`)

// jsonDoc holds formatted JSON lines with optional fold regions.
type jsonDoc struct {
	lines      []string
	foldEnd    map[int]int // start line -> end line (exclusive) when folded
	folded     map[int]bool
	pathFilter string
	filtered   []byte
}

func buildJSONDoc(body []byte, path string) (*jsonDoc, error) {
	src := body
	if path != "" {
		p := strings.TrimSpace(path)
		p = strings.TrimPrefix(p, ".")
		p = strings.TrimPrefix(p, "$.")
		p = jsonPathIndex.ReplaceAllString(p, ".$1")
		if p != "" {
			r := gjson.GetBytes(body, p)
			if !r.Exists() {
				src = []byte("{}")
			} else {
				src = []byte(r.Raw)
			}
		}
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, src, "", "  "); err != nil {
		return nil, err
	}
	doc := &jsonDoc{foldEnd: map[int]int{}, folded: map[int]bool{}}
	doc.lines = strings.Split(buf.String(), "\n")
	doc.pathFilter = path
	doc.filtered = append([]byte(nil), src...)
	doc.indexFolds()
	return doc, nil
}

func (d *jsonDoc) indexFolds() {
	stack := []int{}
	for i, line := range d.lines {
		trim := strings.TrimSpace(line)
		if strings.HasSuffix(trim, "{") || strings.HasSuffix(trim, "[") {
			stack = append(stack, i)
		}
		if strings.HasPrefix(trim, "}") || strings.HasPrefix(trim, "]") {
			if len(stack) > 0 {
				start := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				d.foldEnd[start] = i + 1
			}
		}
	}
}

func (d *jsonDoc) visibleLineCount() int {
	n := 0
	for i := 0; i < len(d.lines); {
		n++
		if d.folded[i] {
			if end, ok := d.foldEnd[i]; ok {
				i = end
				continue
			}
		}
		i++
	}
	return n
}

func (d *jsonDoc) visibleLine(vis int) (lineNum int, text string, ok bool) {
	n := 0
	for i := 0; i < len(d.lines); {
		if n == vis {
			return i + 1, d.lines[i], true
		}
		n++
		if d.folded[i] {
			if end, ok := d.foldEnd[i]; ok {
				i = end
				continue
			}
		}
		i++
	}
	return 0, "", false
}

func (d *jsonDoc) toggleFold(lineIdx int) {
	if _, ok := d.foldEnd[lineIdx]; ok {
		d.folded[lineIdx] = !d.folded[lineIdx]
	}
}

func (d *jsonDoc) foldAt(lineIdx int, collapse bool) {
	if _, ok := d.foldEnd[lineIdx]; ok {
		d.folded[lineIdx] = collapse
	}
}
