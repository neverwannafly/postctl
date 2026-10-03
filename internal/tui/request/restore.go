package request

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	hist "github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
)

// applyHistoryTemplate merges tmpl into the open request when IDs match.
func (p *Pane) applyHistoryTemplate(msg tui.ApplyRequestPatchMsg) tea.Cmd {
	if p.req == nil || p.req.ID != msg.RequestID {
		return nil
	}
	pre := p.req.Clone()
	kept := applyTemplate(p.req, &msg.Template)
	p.reloadAll()
	note := "Restored from history"
	if len(kept) > 0 {
		note = "Kept current: " + strings.Join(kept, ", ")
	}
	return tea.Batch(p.record(pre, true), p.setNotice(note))
}

func applyTemplate(req *model.Request, tmpl *hist.RequestTemplate) []string {
	var kept []string
	if tmpl.Method != "" {
		req.Method = tmpl.Method
	}
	if tmpl.URL != "" {
		req.URL.Raw = tmpl.URL
	}
	if len(tmpl.Query) > 0 {
		req.URL.Query = cloneKV(tmpl.Query)
	}
	if len(tmpl.PathVars) > 0 {
		req.URL.PathVars = cloneVars(tmpl.PathVars)
	}
	if len(tmpl.Headers) > 0 {
		req.Headers = mergeKV(req.Headers, tmpl.Headers, &kept, "header ")
	}
	if tmpl.BodyMode != "" || tmpl.BodyRaw != "" || len(tmpl.BodyURLEncoded) > 0 || len(tmpl.BodyFormData) > 0 {
		applyBodyTemplate(&req.Body, tmpl, &kept)
	}
	return kept
}

func applyBodyTemplate(b *model.Body, tmpl *hist.RequestTemplate, kept *[]string) {
	if tmpl.BodyTruncated {
		*kept = append(*kept, "body (truncated)")
		return
	}
	if tmpl.BodyMode != "" {
		b.Mode = tmpl.BodyMode
	}
	switch tmpl.BodyMode {
	case model.BodyRaw:
		if hist.IsMasked(tmpl.BodyRaw) {
			*kept = append(*kept, "body")
		} else {
			b.Raw = tmpl.BodyRaw
		}
	case model.BodyURLEncoded:
		b.URLEncoded = mergeKV(b.URLEncoded, tmpl.BodyURLEncoded, kept, "field ")
	case model.BodyFormData:
		b.FormData = mergeForm(b.FormData, tmpl.BodyFormData, kept)
	case model.BodyGraphQL:
		if b.GraphQL == nil {
			b.GraphQL = &model.GraphQLBody{}
		}
		if hist.IsMasked(tmpl.BodyRaw) {
			*kept = append(*kept, "GraphQL query")
		} else {
			b.GraphQL.Query = tmpl.BodyRaw
		}
		if tmpl.BodyGraphQLVariables != "" {
			if hist.IsMasked(tmpl.BodyGraphQLVariables) {
				*kept = append(*kept, "GraphQL variables")
			} else {
				b.GraphQL.Variables = tmpl.BodyGraphQLVariables
			}
		}
	case model.BodyBinary:
		if tmpl.BodyRaw != "" && !hist.IsMasked(tmpl.BodyRaw) {
			b.File = tmpl.BodyRaw
		}
	}
}

func mergeKV(cur, tmpl []model.KV, kept *[]string, prefix string) []model.KV {
	if len(tmpl) == 0 {
		return cur
	}
	byKey := map[string]int{}
	for i, kv := range cur {
		byKey[strings.ToLower(kv.Key)] = i
	}
	out := append([]model.KV(nil), cur...)
	for _, kv := range tmpl {
		if hist.IsMasked(kv.Value) {
			*kept = append(*kept, prefix+kv.Key)
			continue
		}
		if i, ok := byKey[strings.ToLower(kv.Key)]; ok {
			out[i].Value = kv.Value
			out[i].Disabled = kv.Disabled
		} else {
			out = append(out, kv)
		}
	}
	return out
}

func mergeForm(cur []model.FormPart, tmpl []hist.TemplateFormPart, kept *[]string) []model.FormPart {
	if len(tmpl) == 0 {
		return cur
	}
	byKey := map[string]int{}
	for i, p := range cur {
		byKey[strings.ToLower(p.Key)] = i
	}
	out := append([]model.FormPart(nil), cur...)
	for _, tp := range tmpl {
		valMasked := hist.IsMasked(tp.Value)
		if valMasked && tp.Type != model.FormFile {
			*kept = append(*kept, "field "+tp.Key)
			continue
		}
		part := model.FormPart{Key: tp.Key, Type: tp.Type, Disabled: tp.Disabled}
		if tp.Type == model.FormFile {
			part.Src = tp.Src
		} else {
			part.Value = tp.Value
		}
		if i, ok := byKey[strings.ToLower(tp.Key)]; ok {
			out[i] = part
		} else {
			out = append(out, part)
		}
	}
	return out
}

func cloneKV(in []model.KV) []model.KV {
	out := make([]model.KV, len(in))
	copy(out, in)
	return out
}

func cloneVars(in []model.Variable) []model.Variable {
	out := make([]model.Variable, len(in))
	copy(out, in)
	return out
}

// FormatKeptFields joins kept field names for tests and status text.
func FormatKeptFields(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return fmt.Sprintf("Kept current: %s", strings.Join(names, ", "))
}
