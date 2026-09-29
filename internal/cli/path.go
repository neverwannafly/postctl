package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
)

// node is one collection, folder or request addressed by a path.
type node struct {
	col   *model.Collection
	item  model.Item // nil when the node is the collection itself
	chain []model.Container
	names []string
}

func (n node) kind() string {
	if n.item == nil {
		return "collection"
	}
	switch n.item.(type) {
	case *model.Folder:
		return "folder"
	default:
		return "request"
	}
}

func (n node) id() model.ID {
	if n.item == nil {
		return n.col.ID
	}
	return n.item.ItemID()
}

func (n node) name() string {
	if n.item == nil {
		return n.col.Name
	}
	return n.item.ItemName()
}

func (n node) path() string { return strings.Join(n.names, "/") }

func (n node) request() *model.Request {
	r, _ := n.item.(*model.Request)
	return r
}

func (n node) folder() *model.Folder {
	f, _ := n.item.(*model.Folder)
	return f
}

// containers is the chain ResolveRequest expects: the collection and ancestor
// folders. A request is not included. A folder node includes itself.
func (n node) containers() []model.Container {
	if n.kind() == "request" {
		return n.chain
	}
	if n.item == nil {
		return []model.Container{n.col}
	}
	return n.chain
}

type pathError struct {
	msg string
}

func (e *pathError) Error() string { return e.msg }

// resolvePath finds the node named by path. path is collection/folder/.../name,
// matched case-insensitively by display name, slug or ID. A single segment
// that matches nothing at the collection level is also searched through the
// whole tree, so a unique request name or ID works on its own.
func resolvePath(ws *model.Workspace, raw string) (node, error) {
	parts := splitPath(raw)
	if len(parts) == 0 {
		return node{}, &pathError{msg: "Missing path."}
	}
	var roots []node
	for _, c := range ws.Collections {
		roots = append(roots, collectionNode(c))
	}
	matched := filterNodes(roots, parts[0])
	if len(matched) == 0 && len(parts) == 1 {
		matched = searchTree(ws, parts[0])
	}
	if len(matched) == 0 {
		return node{}, &pathError{msg: fmt.Sprintf("Nothing matches %q.", raw)}
	}
	for _, part := range parts[1:] {
		var next []node
		for _, m := range matched {
			next = append(next, filterNodes(children(m), part)...)
		}
		if len(next) == 0 {
			return node{}, &pathError{msg: fmt.Sprintf("Nothing matches %q.", raw)}
		}
		matched = next
	}
	matched = uniqueNodes(matched)
	if len(matched) > 1 {
		return node{}, &pathError{msg: ambiguity(raw, matched)}
	}
	return matched[0], nil
}

func ambiguity(raw string, nodes []node) string {
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].path() < nodes[j].path() })
	var b strings.Builder
	fmt.Fprintf(&b, "%q matches more than one item:\n", raw)
	for _, n := range nodes {
		fmt.Fprintf(&b, "  %s\n", n.path())
	}
	b.WriteString("Pass a longer path.")
	return b.String()
}

func splitPath(raw string) []string {
	var parts []string
	for _, p := range strings.Split(raw, "/") {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func collectionNode(c *model.Collection) node {
	return node{col: c, chain: []model.Container{c}, names: []string{c.Name}}
}

func children(n node) []node {
	parent := n.container()
	if parent == nil {
		return nil
	}
	chain := append([]model.Container{}, n.containers()...)
	var out []node
	for _, it := range parent.Children() {
		names := append(append([]string{}, n.names...), it.ItemName())
		switch v := it.(type) {
		case *model.Folder:
			out = append(out, node{
				col: n.col, item: v,
				chain: append(append([]model.Container{}, chain...), v),
				names: names,
			})
		case *model.Request:
			out = append(out, node{col: n.col, item: v, chain: chain, names: names})
		}
	}
	return out
}

func (n node) container() model.Container {
	switch n.kind() {
	case "collection":
		return n.col
	case "folder":
		return n.folder()
	default:
		return nil
	}
}

func filterNodes(nodes []node, segment string) []node {
	var out []node
	for _, n := range nodes {
		if nameMatch(n.name(), string(n.id()), segment) {
			out = append(out, n)
		}
	}
	return out
}

func searchTree(ws *model.Workspace, segment string) []node {
	var hits []node
	for _, c := range ws.Collections {
		hits = append(hits, walkMatch(c, segment)...)
	}
	return hits
}

func walkMatch(col *model.Collection, segment string) []node {
	var hits []node
	var walk func(parent model.Container, chain []model.Container, names []string)
	walk = func(parent model.Container, chain []model.Container, names []string) {
		for _, it := range parent.Children() {
			childNames := append(append([]string{}, names...), it.ItemName())
			switch v := it.(type) {
			case *model.Folder:
				childChain := append(append([]model.Container{}, chain...), v)
				if nameMatch(v.Name, string(v.ID), segment) {
					hits = append(hits, node{col: col, item: v, chain: childChain, names: childNames})
				}
				walk(v, childChain, childNames)
			case *model.Request:
				if nameMatch(v.Name, string(v.ID), segment) {
					hits = append(hits, node{col: col, item: v, chain: chain, names: childNames})
				}
			}
		}
	}
	walk(col, []model.Container{col}, []string{col.Name})
	return hits
}

func uniqueNodes(nodes []node) []node {
	seen := map[string]bool{}
	var out []node
	for _, n := range nodes {
		key := n.kind() + "\x00" + string(n.id())
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, n)
	}
	return out
}

// nameMatch reports whether segment addresses name or id by display name,
// slug, or ID. Comparison is case-insensitive.
func nameMatch(name, id, segment string) bool {
	if segment == "" {
		return false
	}
	if id != "" && strings.EqualFold(id, segment) {
		return true
	}
	if strings.EqualFold(name, segment) {
		return true
	}
	slug := store.Slug(name, "")
	if slug == "" {
		return false
	}
	if strings.EqualFold(slug, segment) {
		return true
	}
	return slug == store.Slug(segment, "")
}
