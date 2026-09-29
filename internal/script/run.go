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
	interruptedMemory
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
	br := &bridge{
		ctx: runCtx, in: in, res: res, loop: loop, now: time.Now,
		slots: make(chan struct{}, sendConcurrency),
	}

	// The loop owns its Runtime, so borrow the pointer from an empty turn.
	// Runtime.Interrupt is the one method documented as safe to call from
	// another goroutine, which is what the watchdog needs.
	var vm *goja.Runtime
	loop.Run(func(r *goja.Runtime) { vm = r })
	vm.SetMaxCallStackSize(maxCallDepth)

	// The VM runs on its own goroutine so that run can return even if the
	// VM never comes back: Interrupt only takes effect between bytecode
	// instructions, so a long native call (a huge Array.prototype.fill, a
	// regexp2 match) is out of its reach.
	done := make(chan error, 1)
	go func() { done <- execute(runCtx, vm, loop, br, boot, units, in) }()

	select {
	case fatal := <-done:
		return outcome(res, fatal)
	case <-runCtx.Done():
	}

	grace := time.NewTimer(abandonGrace)
	defer grace.Stop()
	select {
	case fatal := <-done:
		return outcome(res, fatal)
	case <-grace.C:
	}

	// The VM ignored the interrupt. Freeze what it recorded and leave it
	// behind; the bridge refuses every later write, so the abandoned
	// goroutine can no longer touch the variables, cookies or the result.
	out := br.abandon()
	out.TimedOut = true
	if out.Err == nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			setTimeoutError(out, in.Timeout)
		} else {
			out.Err = &ScriptError{File: units[0].file, Name: "Error", Message: "script run cancelled"}
		}
	}
	return out, nil
}

func outcome(res *Result, fatal error) (*Result, error) {
	if fatal != nil {
		return nil, fatal
	}
	return res, nil
}

// execute runs the chain on the VM and the wrap-up after it. It owns the
// event loop until it returns.
func execute(runCtx context.Context, vm *goja.Runtime, loop *eventloop.EventLoop, br *bridge, boot *goja.Program, units []unit, in Input) (fatal error) {
	defer loop.Terminate()

	var reason atomic.Int32
	finished := make(chan struct{})
	watchdogDone := make(chan struct{})
	go func() {
		defer close(watchdogDone)
		watch(runCtx, finished, vm, loop, newMemoryWatch(in.MemoryLimit), &reason)
	}()

	loop.Run(func(r *goja.Runtime) {
		defer func() {
			if p := recover(); p != nil {
				if _, ok := p.(*goja.InterruptedError); ok {
					return
				}
				fatal = fmt.Errorf("%w: sandbox panic: %v", ErrNoRuntime, p)
			}
		}()
		fatal = start(r, boot, br, units)
	})
	close(finished)
	// Wait for the watchdog so it cannot Interrupt after the ClearInterrupt
	// below.
	<-watchdogDone

	switch reason.Load() {
	case interruptedTimeout:
		vm.ClearInterrupt()
		br.withResult(func(res *Result) {
			res.TimedOut = true
			setTimeoutError(res, in.Timeout)
		})
	case interruptedContext:
		vm.ClearInterrupt()
		br.withResult(func(res *Result) {
			res.TimedOut = true
			if res.Err == nil {
				res.Err = &ScriptError{File: units[0].file, Name: "Error", Message: "script run cancelled"}
			}
		})
	case interruptedMemory:
		vm.ClearInterrupt()
		br.withResult(func(res *Result) {
			if res.Err == nil {
				res.Err = &ScriptError{
					File: units[0].file, Name: "Error",
					Message: fmt.Sprintf("script exceeded the memory limit of %d MiB", memoryLimit(in.MemoryLimit)>>20),
				}
			}
		})
	}

	if fatal != nil {
		return fatal
	}

	// The loop has stopped, so the VM belongs to this goroutine again. It
	// cannot be restarted (Terminate leaves cancelled jobs counted), so the
	// wrap-up runs directly; it schedules nothing. It can still call script
	// code (an overridden pm.request.toJSON, say), so it gets its own budget.
	wrapUp(vm, br, units, in)
	return nil
}

// wrapUp runs finish under the finishBudget deadline.
func wrapUp(vm *goja.Runtime, br *bridge, units []unit, in Input) {
	fired := make(chan struct{})
	var expired atomic.Bool
	timer := time.AfterFunc(finishBudget, func() {
		defer close(fired)
		expired.Store(true)
		vm.Interrupt("postctl: script wrap-up stopped")
	})
	finish(vm, br, units, in)
	if !timer.Stop() {
		<-fired
	}
	if !expired.Load() {
		return
	}
	vm.ClearInterrupt()
	br.withResult(func(res *Result) {
		res.TimedOut = true
		if res.Err == nil {
			res.Err = &ScriptError{
				File: units[0].file, Name: "Error",
				Message: fmt.Sprintf("script wrap-up timed out after %s", finishBudget),
			}
		}
	})
}

// watch stops the VM when the run's context ends or the heap outgrows the
// memory limit, whichever comes first, and returns once it has interrupted the
// VM or finished is closed.
func watch(runCtx context.Context, finished <-chan struct{}, vm *goja.Runtime, loop *eventloop.EventLoop, mem *memoryWatch, reason *atomic.Int32) {
	var tick <-chan time.Time
	if mem != nil {
		t := time.NewTicker(memoryPollInterval)
		defer t.Stop()
		tick = t.C
	}
	stop := func(why int32) {
		reason.Store(why)
		vm.Interrupt("postctl: script run stopped")
		loop.StopNoWait()
	}
	for {
		select {
		case <-finished:
			return
		case <-runCtx.Done():
			if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
				stop(interruptedTimeout)
			} else {
				stop(interruptedContext)
			}
			return
		case <-tick:
			if mem.exceeded() {
				stop(interruptedMemory)
				return
			}
		}
	}
}

func memoryLimit(configured int64) int64 {
	if configured == 0 {
		return DefaultMemoryLimit
	}
	return configured
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
func start(vm *goja.Runtime, boot *goja.Program, br *bridge, units []unit) error {
	buffer.Enable(vm)

	if _, err := vm.RunProgram(boot); err != nil {
		return fmt.Errorf("%w: %w", ErrNoRuntime, err)
	}

	// The bootstrap publishes its entry points on the global object because
	// that is the only place an IIFE can put them. Capture them and delete
	// the globals before any user code is compiled, so a script can neither
	// call nor replace them (the bridge itself, `host`, is a variable inside
	// the sandbox's function scope and was never a global).
	entry, err := takeEntryPoints(vm)
	if err != nil {
		return err
	}
	br.finishFn = entry.finish

	hostObj, err := br.object(vm)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNoRuntime, err)
	}
	ctxJSON, err := br.contextJSON()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNoRuntime, err)
	}

	if _, err := entry.init(goja.Undefined(), hostObj, vm.ToValue(ctxJSON)); err != nil {
		return fmt.Errorf("%w: %w", ErrNoRuntime, err)
	}

	fns := make([]any, 0, len(units))
	for _, u := range units {
		prog, cerr := userProgram(u.file, u.source)
		if cerr != nil {
			br.withResult(func(res *Result) { res.Err = compileError(u, cerr) })
			return nil
		}
		v, rerr := vm.RunProgram(prog)
		if rerr != nil {
			br.withResult(func(res *Result) { res.Err = runtimeError(u, rerr) })
			return nil
		}
		fns = append(fns, v)
	}

	if _, err := entry.run(goja.Undefined(), vm.ToValue(fns)); err != nil {
		if isInterrupt(err) {
			return nil
		}
		var overflow *goja.StackOverflowError
		if errors.As(err, &overflow) {
			br.withResult(func(res *Result) {
				if res.Err == nil {
					res.Err = stackOverflowError(units, err)
				}
			})
			return nil
		}
		return fmt.Errorf("%w: %w", ErrNoRuntime, err)
	}
	return nil
}

// entryPoints are the sandbox bootstrap functions the runner calls.
type entryPoints struct {
	init, run, finish goja.Callable
}

// takeEntryPoints moves the sandbox's bootstrap functions off the global
// object and returns them.
func takeEntryPoints(vm *goja.Runtime) (entryPoints, error) {
	var out entryPoints
	global := vm.GlobalObject()
	for _, e := range []struct {
		name string
		dst  *goja.Callable
	}{
		{"__postctl_init", &out.init},
		{"__postctl_run", &out.run},
		{"__postctl_finish", &out.finish},
	} {
		fn, ok := goja.AssertFunction(global.Get(e.name))
		if !ok {
			return out, fmt.Errorf("%w: %s missing", ErrNoRuntime, e.name)
		}
		*e.dst = fn
		if err := global.Delete(e.name); err != nil {
			return out, fmt.Errorf("%w: %w", ErrNoRuntime, err)
		}
	}
	return out, nil
}

// stackOverflowError describes a call that nested deeper than maxCallDepth.
// goja reports it as an uncatchable Go error rather than a JavaScript
// exception, so a script cannot catch it either. The error text is the stack
// trace, which names the script the recursion happened in.
func stackOverflowError(units []unit, err error) *ScriptError {
	stack := err.Error()
	u := units[0]
	for _, c := range units {
		if strings.Contains(stack, c.file) {
			u = c
			break
		}
	}
	se := &ScriptError{
		ScriptIndex: u.index, File: u.file, Name: "RangeError",
		Message: "Maximum call stack size exceeded", Stack: stack,
	}
	se.Line, se.Column = firstFrame(stack, u.file)
	return se
}

// finish settles unfinished tests, folds in the legacy tests object and reads
// the possibly mutated request back. It runs after the loop has drained, so a
// script's asynchronous work is already accounted for.
func finish(vm *goja.Runtime, br *bridge, units []unit, in Input) {
	var payload string
	if fn := br.finishFn; fn != nil {
		if v, err := fn(goja.Undefined()); err == nil {
			payload = v.String()
		}
	}

	br.withResult(func(res *Result) {
		// Map the error the sandbox reported (which carries a script
		// index, not a file) onto the script that raised it.
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
	})
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
	se.Line, se.Column = syntaxPosition(se.Message)
	return se
}

// syntaxErrorPattern matches the "Line 2:11" position goja's compiler writes
// into its message.
var syntaxErrorPattern = regexp.MustCompile(`Line (\d+):(\d+)`)

func syntaxPosition(message string) (line, column int) {
	m := syntaxErrorPattern.FindStringSubmatch(message)
	if m == nil {
		return 0, 0
	}
	l, err1 := strconv.Atoi(m[1])
	c, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil {
		return 0, 0
	}
	if l == 1 {
		c -= len(scriptPrefix)
		if c < 1 {
			c = 1
		}
	}
	return l, c
}

func runtimeError(u unit, err error) *ScriptError {
	var overflow *goja.StackOverflowError
	if errors.As(err, &overflow) {
		return stackOverflowError([]unit{u}, err)
	}
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
