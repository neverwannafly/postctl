package script

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/buffer"
	"github.com/dop251/goja_nodejs/eventloop"

	"github.com/shubham/postctl/internal/model"
)

// interrupt reasons, stored atomically so the watchdog goroutine and the loop
// goroutine can agree on why a run stopped.
const (
	notInterrupted int32 = iota
	interruptedTimeout
	interruptedContext
)

// unit is one runnable script in the chain.
type unit struct {
	index  int
	file   string
	source string
}

func runnable(scripts []*model.Script, event Event) []unit {
	var out []unit
	for i, s := range scripts {
		if s == nil || s.Disabled || strings.TrimSpace(s.Source) == "" {
			continue
		}
		file := s.File
		if file == "" {
			file = fmt.Sprintf("%s[%d].js", event, i)
		}
		out = append(out, unit{index: i, file: file, source: s.Source})
	}
	return out
}

func run(ctx context.Context, scripts []*model.Script, in Input) (*Result, error) {
	res := &Result{Request: cloneRequest(in.Request)}

	units := runnable(scripts, in.Event)
	if len(units) == 0 {
		return res, nil
	}

	boot, err := bootProgram()
	if err != nil {
		return nil, err
	}

	runCtx, cancel := context.WithTimeout(ctx, in.Timeout)
	defer cancel()

	loop := eventloop.NewEventLoop(eventloop.WithRegistry(registry), eventloop.EnableConsole(false))
	defer loop.Terminate()

	br := &bridge{ctx: runCtx, in: in, res: res, loop: loop, now: time.Now}

	// The loop owns its Runtime, so borrow the pointer from an empty turn.
	// Runtime.Interrupt is the one method documented as safe to call from
	// another goroutine, which is what the watchdog below needs.
	var vm *goja.Runtime
	loop.Run(func(r *goja.Runtime) { vm = r })

	var reason atomic.Int32
	finished := make(chan struct{})
	go func() {
		select {
		case <-finished:
			return
		case <-runCtx.Done():
		}
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			reason.Store(interruptedTimeout)
		} else {
			reason.Store(interruptedContext)
		}
		vm.Interrupt("postctl: script run stopped")
		loop.StopNoWait()
	}()

	var fatal error
	loop.Run(func(r *goja.Runtime) {
		defer func() {
			if p := recover(); p != nil {
				if _, ok := p.(*goja.InterruptedError); ok {
					return
				}
				fatal = fmt.Errorf("%w: sandbox panic: %v", ErrNoRuntime, p)
			}
		}()
		fatal = start(r, boot, br, units, res)
	})
	close(finished)

	switch reason.Load() {
	case interruptedTimeout:
		res.TimedOut = true
		vm.ClearInterrupt()
		setTimeoutError(res, in.Timeout)
	case interruptedContext:
		res.TimedOut = true
		vm.ClearInterrupt()
		if res.Err == nil {
			res.Err = &ScriptError{File: units[0].file, Name: "Error", Message: "script run cancelled"}
		}
	}

	if fatal != nil {
		return nil, fatal
	}

	// The loop has stopped, so the VM belongs to this goroutine again. It
	// cannot be restarted (Terminate leaves cancelled jobs counted), so the
	// wrap-up runs directly; it schedules nothing.
	finish(vm, res, units, in)
	return res, nil
}

func setTimeoutError(res *Result, timeout time.Duration) {
	if res.Err != nil {
		return
	}
	res.Err = &ScriptError{
		Name:    "Error",
		Message: fmt.Sprintf("script execution timed out after %s", timeout),
	}
}

// start boots the sandbox and kicks off the chain. It returns a non-nil error
// only for problems with the sandbox itself; script failures land in res.
func start(vm *goja.Runtime, boot *goja.Program, br *bridge, units []unit, res *Result) error {
	buffer.Enable(vm)

	if _, err := vm.RunProgram(boot); err != nil {
		return fmt.Errorf("%w: %w", ErrNoRuntime, err)
	}

	hostObj, err := br.object(vm)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNoRuntime, err)
	}
	ctxJSON, err := br.contextJSON()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNoRuntime, err)
	}

	initFn, ok := goja.AssertFunction(vm.Get("__postctl_init"))
	if !ok {
		return fmt.Errorf("%w: __postctl_init missing", ErrNoRuntime)
	}
	if _, err := initFn(goja.Undefined(), hostObj, vm.ToValue(ctxJSON)); err != nil {
		return fmt.Errorf("%w: %w", ErrNoRuntime, err)
	}

	fns := make([]any, 0, len(units))
	for _, u := range units {
		prog, cerr := userProgram(u.file, u.source)
		if cerr != nil {
			res.Err = compileError(u, cerr)
			return nil
		}
		v, rerr := vm.RunProgram(prog)
		if rerr != nil {
			res.Err = runtimeError(u, rerr)
			return nil
		}
		fns = append(fns, v)
	}

	runFn, ok := goja.AssertFunction(vm.Get("__postctl_run"))
	if !ok {
		return fmt.Errorf("%w: __postctl_run missing", ErrNoRuntime)
	}
	if _, err := runFn(goja.Undefined(), vm.ToValue(fns)); err != nil {
		if isInterrupt(err) {
			return nil
		}
		return fmt.Errorf("%w: %w", ErrNoRuntime, err)
	}
	return nil
}

// finish settles unfinished tests, folds in the legacy tests object and reads
// the possibly mutated request back. It runs after the loop has drained, so a
// script's asynchronous work is already accounted for.
func finish(vm *goja.Runtime, res *Result, units []unit, in Input) {
	var payload string
	if fn, ok := goja.AssertFunction(vm.Get("__postctl_finish")); ok {
		if v, err := fn(goja.Undefined()); err == nil {
			payload = v.String()
		}
	}

	// Map the error the sandbox reported (which carries a script index, not
	// a file) onto the script that raised it.
	if res.Err != nil && res.Err.File == "" {
		i := res.Err.ScriptIndex
		if i >= 0 && i < len(units) {
			res.Err.File = units[i].file
			res.Err.ScriptIndex = units[i].index
			res.Err.Line, res.Err.Column = firstFrame(res.Err.Stack, units[i].file)
		}
	}

	if payload == "" || in.Event != EventPreRequest {
		return
	}
	var out struct {
		Request requestDef `json:"request"`
	}
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		return
	}
	res.Request = applyDef(in.Request, out.Request)
}

func cloneRequest(r *model.Request) *model.Request {
	if r == nil {
		return nil
	}
	out := *r
	out.Headers = append([]model.KV(nil), r.Headers...)
	out.URL.Query = append([]model.KV(nil), r.URL.Query...)
	out.URL.PathVars = append([]model.Variable(nil), r.URL.PathVars...)
	out.Body.URLEncoded = append([]model.KV(nil), r.Body.URLEncoded...)
	out.Body.FormData = append([]model.FormPart(nil), r.Body.FormData...)
	return &out
}

func isInterrupt(err error) bool {
	var ie *goja.InterruptedError
	return errors.As(err, &ie)
}

func compileError(u unit, err error) *ScriptError {
	se := &ScriptError{ScriptIndex: u.index, File: u.file, Name: "SyntaxError", Message: err.Error()}
	var ce *goja.CompilerSyntaxError
	if errors.As(err, &ce) {
		se.Message = ce.Error()
	}
	se.Message = strings.TrimPrefix(se.Message, "SyntaxError: ")
	se.Line, se.Column = firstFrame(se.Message, u.file)
	return se
}

func runtimeError(u unit, err error) *ScriptError {
	se := &ScriptError{ScriptIndex: u.index, File: u.file, Name: "Error", Message: err.Error()}
	var ex *goja.Exception
	if errors.As(err, &ex) {
		se.Stack = ex.String()
		if obj, ok := ex.Value().(*goja.Object); ok {
			if n := obj.Get("name"); n != nil {
				se.Name = n.String()
			}
			if m := obj.Get("message"); m != nil {
				se.Message = m.String()
			}
		}
	}
	se.Line, se.Column = firstFrame(se.Stack, u.file)
	return se
}

// framePattern matches the "at file:line:column" frames goja writes, and the
// "file:line:column" prefix of a syntax error.
var framePattern = regexp.MustCompile(`([^\s(]+):(\d+):(\d+)`)

// firstFrame finds the innermost frame that belongs to file and maps it back
// to a position in the original script. The sandbox wraps every script body
// in a one-line prefix, so only column numbers on line 1 need adjusting.
func firstFrame(stack, file string) (line, column int) {
	for _, m := range framePattern.FindAllStringSubmatch(stack, -1) {
		if !strings.HasSuffix(m[1], file) {
			continue
		}
		l, err1 := strconv.Atoi(m[2])
		c, err2 := strconv.Atoi(m[3])
		if err1 != nil || err2 != nil {
			continue
		}
		if l == 1 {
			c -= len(scriptPrefix)
			if c < 1 {
				c = 1
			}
		}
		return l, c
	}
	return 0, 0
}
