package response

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
)

var jsonPathIndex = regexp.MustCompile(`\[(\d+)\]`)

// jsonDoc holds formatted JSON lines, optionally narrowed to a path.
type jsonDoc struct {
	lines      []string
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
	doc := &jsonDoc{}
	doc.lines = strings.Split(buf.String(), "\n")
	doc.pathFilter = path
	doc.filtered = append([]byte(nil), src...)
	return doc, nil
}
