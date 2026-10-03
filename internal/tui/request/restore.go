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
		p.storePendingPatch(msg)
		return nil
	}
	return p.applyHistoryTemplateNow(msg)
}

func (p *Pane) applyHistoryTemplateNow(msg tui.ApplyRequestPatchMsg) tea.Cmd {
	pre := p.req.Clone()
	kept, err := applyTemplate(p.req, &msg.Template)
	p.reloadAll()
	if err != "" {
		return p.setNotice("Restore failed: " + err)
	}
	note := "Restored from history"
	if len(kept) > 0 {
		note = "Kept current: " + strings.Join(kept, ", ")
	}
	return tea.Batch(p.record(pre, true), p.setNotice(note))
}

func (p *Pane) storePendingPatch(msg tui.ApplyRequestPatchMsg) {
	if p.pending == nil {
		p.pending = map[model.ID]tui.ApplyRequestPatchMsg{}
	}
	p.pending[msg.RequestID] = msg
}

func (p *Pane) takePendingPatch(id model.ID) (tui.ApplyRequestPatchMsg, bool) {
	if p.pending == nil {
		return tui.ApplyRequestPatchMsg{}, false
	}
	msg, ok := p.pending[id]
	if ok {
		delete(p.pending, id)
	}
	return msg, ok
}

func applyTemplate(req *model.Request, tmpl *hist.RequestTemplate) (kept []string, errMsg string) {
	if tmpl == nil {
		return nil, "empty template"
	}
	cur := req.Clone()
	if tmpl.Method != "" {
		req.Method = tmpl.Method
	}
	// URL
	if hist.FieldMasked(tmpl.URLMasked, tmpl.URL) {
		kept = append(kept, "URL")
	} else if hist.RestoreSafeValue(tmpl.URL) || tmpl.URL == "" {
		req.URL.Raw = tmpl.URL
	} else {
		kept = append(kept, "URL")
	}
	req.URL.Query = applyTemplateKV(cur.URL.Query, tmpl.Query, "query ", &kept)
	req.URL.PathVars = applyTemplatePathVars(cur.URL.PathVars, tmpl.PathVars, &kept)
	req.Headers = applyTemplateKV(cur.Headers, tmpl.Headers, "header ", &kept)
	applyBodyTemplate(&req.Body, cur.Body, tmpl, &kept)
	return kept, ""
}

func applyTemplateKV(cur []model.KV, tmpl []hist.TemplateKV, prefix string, kept *[]string) []model.KV {
	curBy := kvByKey(cur)
	out := make([]model.KV, 0, len(tmpl))
	for _, tk := range tmpl {
		masked := hist.FieldMasked(tk.Masked, tk.Value)
		kv := model.KV{Key: tk.Key, Value: tk.Value, Disabled: tk.Disabled}
		if masked || !hist.RestoreSafeValue(tk.Value) {
			if old, ok := curBy[strings.ToLower(tk.Key)]; ok {
				kv.Value = old.Value
			} else {
				kv.Value = ""
			}
			*kept = append(*kept, prefix+tk.Key)
		}
		out = append(out, kv)
	}
	return out
}

func applyTemplatePathVars(cur []model.Variable, tmpl []hist.TemplateVariable, kept *[]string) []model.Variable {
	curBy := map[string]model.Variable{}
	for _, v := range cur {
		curBy[strings.ToLower(v.Key)] = v
	}
	out := make([]model.Variable, 0, len(tmpl))
	for _, tv := range tmpl {
		masked := hist.FieldMasked(tv.Masked, tv.Value)
		v := model.Variable{Key: tv.Key, Value: tv.Value}
		if masked || !hist.RestoreSafeValue(tv.Value) {
			if old, ok := curBy[strings.ToLower(tv.Key)]; ok {
				v.Value = old.Value
			} else {
				v.Value = ""
			}
			*kept = append(*kept, "path "+tv.Key)
		}
		out = append(out, v)
	}
	return out
}

func applyBodyTemplate(b *model.Body, cur model.Body, tmpl *hist.RequestTemplate, kept *[]string) {
	if tmpl.BodyMode != "" {
		b.Mode = tmpl.BodyMode
	}
	switch tmpl.BodyMode {
	case model.BodyRaw:
		if hist.FieldMasked(tmpl.BodyRawMasked, tmpl.BodyRaw) || tmpl.BodyRawTruncated || tmpl.BodyRawEncoding != "" {
			*kept = append(*kept, bodyKeepLabel(tmpl))
		} else if hist.RestoreSafeValue(tmpl.BodyRaw) || tmpl.BodyRaw == "" {
			b.Raw = tmpl.BodyRaw
		} else {
			*kept = append(*kept, "body")
		}
	case model.BodyURLEncoded:
		b.URLEncoded = applyTemplateKV(cur.URLEncoded, tmpl.BodyURLEncoded, "field ", kept)
	case model.BodyFormData:
		b.FormData = applyTemplateForm(cur.FormData, tmpl.BodyFormData, kept)
	case model.BodyGraphQL:
		if b.GraphQL == nil {
			b.GraphQL = &model.GraphQLBody{}
		}
		if hist.FieldMasked(tmpl.BodyRawMasked, tmpl.BodyRaw) || tmpl.BodyRawTruncated || tmpl.BodyRawEncoding != "" {
			*kept = append(*kept, "GraphQL query")
		} else if hist.RestoreSafeValue(tmpl.BodyRaw) || tmpl.BodyRaw == "" {
			b.GraphQL.Query = tmpl.BodyRaw
		} else {
			*kept = append(*kept, "GraphQL query")
		}
		if tmpl.BodyGraphQLVariables != "" || tmpl.BodyGraphQLVariablesMasked || tmpl.BodyGraphQLVariablesTruncated {
			if hist.FieldMasked(tmpl.BodyGraphQLVariablesMasked, tmpl.BodyGraphQLVariables) ||
				tmpl.BodyGraphQLVariablesTruncated || !hist.RestoreSafeValue(tmpl.BodyGraphQLVariables) {
				*kept = append(*kept, "GraphQL variables")
			} else {
				b.GraphQL.Variables = tmpl.BodyGraphQLVariables
			}
		}
	case model.BodyBinary:
		if hist.FieldMasked(tmpl.BodyRawMasked, tmpl.BodyRaw) || !hist.RestoreSafeValue(tmpl.BodyRaw) {
			if tmpl.BodyRaw != "" {
				*kept = append(*kept, "body file")
			}
		} else {
			b.File = tmpl.BodyRaw
		}
	case "":
		if len(tmpl.BodyURLEncoded) == 0 && len(tmpl.BodyFormData) == 0 && tmpl.BodyRaw == "" {
			*b = model.Body{}
		}
	}
}

func bodyKeepLabel(tmpl *hist.RequestTemplate) string {
	if tmpl.BodyRawTruncated {
		return "body (truncated)"
	}
	if tmpl.BodyRawEncoding != "" {
		return "body (binary)"
	}
	return "body"
}

func applyTemplateForm(cur []model.FormPart, tmpl []hist.TemplateFormPart, kept *[]string) []model.FormPart {
	curBy := map[string]model.FormPart{}
	for _, p := range cur {
		curBy[strings.ToLower(p.Key)] = p
	}
	out := make([]model.FormPart, 0, len(tmpl))
	for _, tp := range tmpl {
		part := model.FormPart{Key: tp.Key, Type: tp.Type, Disabled: tp.Disabled}
		if tp.Type == model.FormFile {
			if hist.FieldMasked(tp.SrcMasked, tp.Src) || !hist.RestoreSafeValue(tp.Src) {
				if old, ok := curBy[strings.ToLower(tp.Key)]; ok {
					part.Src = old.Src
				}
				*kept = append(*kept, "field "+tp.Key)
			} else {
				part.Src = tp.Src
			}
		} else {
			if hist.FieldMasked(tp.ValueMasked, tp.Value) || !hist.RestoreSafeValue(tp.Value) {
				if old, ok := curBy[strings.ToLower(tp.Key)]; ok {
					part.Value = old.Value
				}
				*kept = append(*kept, "field "+tp.Key)
			} else {
				part.Value = tp.Value
			}
		}
		out = append(out, part)
	}
	return out
}

func kvByKey(rows []model.KV) map[string]model.KV {
	m := map[string]model.KV{}
	for _, kv := range rows {
		m[strings.ToLower(kv.Key)] = kv
	}
	return m
}

// FormatKeptFields joins kept field names for tests and status text.
func FormatKeptFields(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return fmt.Sprintf("Kept current: %s", strings.Join(names, ", "))
}
