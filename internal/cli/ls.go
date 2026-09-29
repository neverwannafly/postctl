package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func runLs(ctx context.Context, a *app, args []string) int {
	fs := a.flagSet()
	flat := fs.Bool("flat", false, "print full paths, one per line")
	asJSON := fs.Bool("json", false, "print JSON")
	positional, code := a.parse(fs, args)
	if code >= 0 {
		return code
	}
	path, code := a.optionalPath(positional)
	if code >= 0 {
		return code
	}
	if code := a.open(); code >= 0 {
		return code
	}
	_ = ctx
	roots, code := lsRoots(a, path)
	if code >= 0 {
		return code
	}
	items := make([]lsItem, 0, len(roots))
	for _, n := range roots {
		items = append(items, listNode(n, *flat))
	}
	if *asJSON {
		enc := json.NewEncoder(a.out)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(lsDoc{Items: items}); err != nil {
			fmt.Fprintf(a.err, "Cannot write JSON: %v\n", err)
			return ExitRequest
		}
		return ExitOK
	}
	if len(items) == 0 {
		fmt.Fprintln(a.out, "No requests yet.")
		return ExitOK
	}
	for _, it := range items {
		writeTree(a, it, 0, *flat)
	}
	return ExitOK
}

func lsRoots(a *app, path string) ([]node, int) {
	if path == "" {
		var roots []node
		for _, c := range a.ws.Collections {
			roots = append(roots, collectionNode(c))
		}
		return roots, -1
	}
	n, err := resolvePath(a.ws, path)
	if err != nil {
		fmt.Fprintln(a.err, err.Error())
		return nil, ExitUsage
	}
	return []node{n}, -1
}

type lsDoc struct {
	Items []lsItem `json:"items"`
}

type lsItem struct {
	Type     string   `json:"type"`
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	Method   string   `json:"method,omitempty"`
	Children []lsItem `json:"children,omitempty"`
}

func listNode(n node, flat bool) lsItem {
	it := lsItem{Type: n.kind(), ID: string(n.id()), Name: n.name(), Path: n.path()}
	if r := n.request(); r != nil {
		it.Method = r.Method
		return it
	}
	if flat {
		it.Children = flatten(n)
		return it
	}
	for _, c := range children(n) {
		it.Children = append(it.Children, listNode(c, false))
	}
	return it
}

func flatten(n node) []lsItem {
	var out []lsItem
	var walk func(node)
	walk = func(cur node) {
		for _, c := range children(cur) {
			item := lsItem{Type: c.kind(), ID: string(c.id()), Name: c.name(), Path: c.path()}
			if r := c.request(); r != nil {
				item.Method = r.Method
			}
			out = append(out, item)
			if c.kind() != "request" {
				walk(c)
			}
		}
	}
	walk(n)
	return out
}

func writeTree(a *app, it lsItem, depth int, flat bool) {
	if flat {
		if it.Method != "" {
			fmt.Fprintf(a.out, "%s %s\n", a.view.method(it.Method), it.Path)
		} else {
			fmt.Fprintln(a.out, it.Path)
		}
		for _, c := range it.Children {
			writeTree(a, c, 0, true)
		}
		return
	}
	indent := strings.Repeat("  ", depth)
	if it.Method != "" {
		fmt.Fprintf(a.out, "%s%s %s\n", indent, a.view.method(it.Method), it.Name)
		return
	}
	fmt.Fprintf(a.out, "%s%s\n", indent, it.Name)
	for _, c := range it.Children {
		writeTree(a, c, depth+1, false)
	}
}
