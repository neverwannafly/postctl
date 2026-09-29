package script

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"

	"github.com/shubham/postctl/internal/vars"
)

// bridge is the sandbox's only route out of the VM. Every method runs on the
// event loop's goroutine, so the Result and the vars.Store are touched from
// one goroutine only; pm.sendRequest is the single exception and it hands its
// result back through eventloop.RunOnLoop.
type bridge struct {
	ctx  context.Context
	in   Input
	res  *Result
	loop *eventloop.EventLoop
	now  func() time.Time
}

// maxRandomBytes caps one crypto.getRandomValues call.
const maxRandomBytes = 1 << 16

var scopeNames = map[string]vars.Scope{
	"globals":     vars.Global,
	"collection":  vars.Collection,
	"environment": vars.Environment,
	"data":        vars.Data,
	"local":       vars.Local,
}

// exportValue turns a JavaScript value into one of the Go types the vars.Store
// contract allows, so a script reads back the type it stored.
func exportValue(v goja.Value) any {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil
	}
	switch x := v.Export().(type) {
	case int64:
		return float64(x)
	case int:
		return float64(x)
	case string, float64, bool, nil:
		return x
	case []any, map[string]any:
		return x
	case time.Time:
		return x.Format(time.RFC3339Nano)
	default:
		return v.String()
	}
}

func (b *bridge) varGet(scope, key string) any {
	if b.in.Vars == nil {
		return nil
	}
	if s, ok := scopeNames[scope]; ok {
		v, _ := b.in.Vars.GetIn(s, key)
		return v
	}
	v, _, _ := b.in.Vars.Get(key)
	return v
}

func (b *bridge) varSet(call goja.FunctionCall) goja.Value {
	if b.in.Vars == nil {
		return goja.Undefined()
	}
	s, ok := scopeNames[call.Argument(0).String()]
	if !ok {
		s = vars.Local
	}
	b.in.Vars.Set(s, call.Argument(1).String(), exportValue(call.Argument(2)))
	return goja.Undefined()
}

func (b *bridge) varUnset(scope, key string) {
	if b.in.Vars == nil {
		return
	}
	if s, ok := scopeNames[scope]; ok {
		b.in.Vars.Unset(s, key)
	}
}

func (b *bridge) varHas(scope, key string) bool {
	if b.in.Vars == nil {
		return false
	}
	if s, ok := scopeNames[scope]; ok {
		_, found := b.in.Vars.GetIn(s, key)
		return found
	}
	_, _, found := b.in.Vars.Get(key)
	return found
}

func (b *bridge) varClear(scope string) {
	if b.in.Vars == nil {
		return
	}
	if s, ok := scopeNames[scope]; ok {
		b.in.Vars.Clear(s)
	}
}

// allScopes is lowest precedence first, matching vars.Scope's ordering, so a
// merged object ends up with the value a lookup would return.
var allScopes = []vars.Scope{vars.Global, vars.Collection, vars.Environment, vars.Data, vars.Local}

// varObject returns [[key, value], ...] rather than a map. goja hands a Go
// map's keys to JavaScript in an arbitrary order, so a script that serialises
// pm.environment.toObject() would see the key order change between runs; the
// store's own order is the one Postman shows.
func (b *bridge) varObject(scope string) [][2]any {
	out := [][2]any{}
	if b.in.Vars == nil {
		return out
	}
	at := map[string]int{}
	collect := func(s vars.Scope) {
		for _, k := range b.in.Vars.Keys(s) {
			v, ok := b.in.Vars.GetIn(s, k)
			if !ok {
				continue
			}
			if i, seen := at[k]; seen {
				out[i][1] = v
				continue
			}
			at[k] = len(out)
			out = append(out, [2]any{k, v})
		}
	}
	if s, ok := scopeNames[scope]; ok {
		collect(s)
		return out
	}
	for _, s := range allScopes {
		collect(s)
	}
	return out
}

func (b *bridge) varResolve(template string) string {
	if b.in.Vars == nil {
		return template
	}
	return b.in.Vars.Replace(template)
}

func (b *bridge) console(level, message string) {
	b.res.Console = append(b.res.Console, ConsoleEntry{
		Level:   ConsoleLevel(level),
		Message: message,
		Time:    b.now(),
	})
}

func (b *bridge) test(name string, passed, skipped, async bool, errText string, index int) {
	b.res.Tests = append(b.res.Tests, TestResult{
		Name:    name,
		Passed:  passed,
		Skipped: skipped,
		Async:   async,
		Error:   errText,
		Index:   index,
	})
}

func (b *bridge) setNextRequest(name goja.Value) {
	next := ""
	if name != nil && !goja.IsNull(name) && !goja.IsUndefined(name) {
		next = name.String()
	}
	b.res.NextRequest = &next
}

func (b *bridge) skipRequest() { b.res.SkipRequest = true }

func (b *bridge) randomBytes(n int) []int {
	if n < 0 {
		n = 0
	}
	if n > maxRandomBytes {
		n = maxRandomBytes
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand on macOS does not fail, but never hand out zeros
		// silently if it ever does.
		panic(err)
	}
	out := make([]int, n)
	for i, v := range buf {
		out[i] = int(v)
	}
	return out
}

func (b *bridge) sendRequest(defJSON string, cb goja.Callable) {
	fail := func(vm *goja.Runtime, msg string) {
		_, _ = cb(goja.Undefined(), vm.ToValue(msg), goja.Undefined())
	}

	var d requestDef
	if err := json.Unmarshal([]byte(defJSON), &d); err != nil {
		b.laterOnLoop(func(vm *goja.Runtime) { fail(vm, "sendrequest: "+err.Error()) })
		return
	}
	if b.in.Sender == nil {
		b.laterOnLoop(func(vm *goja.Runtime) { fail(vm, "pm.sendRequest is not available: no HTTP sender was configured") })
		return
	}

	req := defToRequest(d)
	ctx := b.ctx
	sender := b.in.Sender
	opts := b.in.SendOptions

	go func() {
		resp, err := sender.Send(ctx, req, opts)
		b.loop.RunOnLoop(func(vm *goja.Runtime) {
			if err != nil {
				fail(vm, err.Error())
				return
			}
			payload, merr := json.Marshal(responseToDef(resp))
			if merr != nil {
				fail(vm, "sendrequest: "+merr.Error())
				return
			}
			_, _ = cb(goja.Undefined(), goja.Null(), vm.ToValue(string(payload)))
		})
	}()
}

// laterOnLoop defers work to the next loop turn so a synchronous failure still
// reaches the script through its callback rather than as a throw.
func (b *bridge) laterOnLoop(fn func(*goja.Runtime)) { b.loop.RunOnLoop(fn) }

func (b *bridge) currentURL() string {
	if b.in.Request == nil {
		return ""
	}
	return b.in.Request.URL.Raw
}

func (b *bridge) cookieList(rawURL string) map[string]any {
	if b.in.Cookies == nil {
		return map[string]any{"error": "no cookie store is available"}
	}
	if rawURL == "" {
		rawURL = b.currentURL()
	}
	cookies, err := b.in.Cookies.Cookies(rawURL)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	out := make([]any, 0, len(cookies))
	for _, c := range cookies {
		out = append(out, cookieToDef(c))
	}
	return map[string]any{"cookies": out}
}

func (b *bridge) cookieSet(rawURL string, def map[string]any) string {
	if b.in.Cookies == nil {
		return "no cookie store is available"
	}
	if err := b.in.Cookies.SetCookie(rawURL, defToCookie(def)); err != nil {
		return err.Error()
	}
	return ""
}

func (b *bridge) cookieUnset(rawURL, name string) string {
	if b.in.Cookies == nil {
		return "no cookie store is available"
	}
	if err := b.in.Cookies.UnsetCookie(rawURL, name); err != nil {
		return err.Error()
	}
	return ""
}

func (b *bridge) cookieClear(rawURL string) string {
	if b.in.Cookies == nil {
		return "no cookie store is available"
	}
	if err := b.in.Cookies.ClearCookies(rawURL); err != nil {
		return err.Error()
	}
	return ""
}

func (b *bridge) scriptError(index int, name, message, stack string) {
	if b.res.Err != nil {
		return
	}
	b.res.Err = &ScriptError{ScriptIndex: index, Name: name, Message: message, Stack: stack}
}

// object builds the host object handed to __postctl_init.
func (b *bridge) object(vm *goja.Runtime) (*goja.Object, error) {
	o := vm.NewObject()
	set := func(name string, fn any) error { return o.Set(name, fn) }

	for name, fn := range map[string]any{
		"varGet":         b.varGet,
		"varSet":         b.varSet,
		"varUnset":       b.varUnset,
		"varHas":         b.varHas,
		"varClear":       b.varClear,
		"varObject":      b.varObject,
		"varResolve":     b.varResolve,
		"console":        b.console,
		"test":           b.test,
		"setNextRequest": b.setNextRequest,
		"skipRequest":    b.skipRequest,
		"randomBytes":    b.randomBytes,
		"sendRequest":    b.sendRequest,
		"cookieList":     b.cookieList,
		"cookieSet":      b.cookieSet,
		"cookieUnset":    b.cookieUnset,
		"cookieClear":    b.cookieClear,
		"scriptError":    b.scriptError,
	} {
		if err := set(name, fn); err != nil {
			return nil, err
		}
	}
	return o, nil
}

// context is the JSON payload __postctl_init parses into pm.info, pm.request,
// pm.response and pm.cookies.
type scriptContext struct {
	EventName      string       `json:"eventName"`
	RequestName    string       `json:"requestName"`
	RequestID      string       `json:"requestId"`
	CollectionName string       `json:"collectionName"`
	Iteration      int          `json:"iteration"`
	IterationCount int          `json:"iterationCount"`
	Location       []string     `json:"location"`
	Request        requestDef   `json:"request"`
	Response       *responseDef `json:"response"`
	Cookies        []any        `json:"cookies"`
}

func (b *bridge) contextJSON() (string, error) {
	c := scriptContext{
		EventName:      string(b.in.Event),
		RequestName:    b.in.Info.RequestName,
		RequestID:      b.in.Info.RequestID,
		CollectionName: b.in.Info.CollectionName,
		Iteration:      b.in.Info.Iteration,
		IterationCount: b.in.Info.IterationCount,
		Request:        requestToDef(b.in.Request),
		Response:       responseToDef(b.in.Response),
	}
	if c.IterationCount == 0 {
		c.IterationCount = 1
	}
	for _, n := range []string{b.in.Info.CollectionName, b.in.Info.RequestName} {
		if n != "" {
			c.Location = append(c.Location, n)
		}
	}
	if b.in.Cookies != nil {
		if cookies, err := b.in.Cookies.Cookies(b.currentURL()); err == nil {
			for _, ck := range cookies {
				c.Cookies = append(c.Cookies, cookieToDef(ck))
			}
		}
	}
	// A test script also sees the cookies the response set.
	if b.in.Response != nil {
		seen := map[string]bool{}
		for _, raw := range c.Cookies {
			if m, ok := raw.(map[string]any); ok {
				seen[toString(m["name"])] = true
			}
		}
		for _, ck := range b.in.Response.Cookies {
			if !seen[ck.Name] {
				c.Cookies = append(c.Cookies, cookieToDef(ck))
			}
		}
	}
	out, err := json.Marshal(c)
	return string(out), err
}
