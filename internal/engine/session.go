package engine

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/oauth2"
	"github.com/shubham/postctl/internal/script"
	"github.com/shubham/postctl/internal/vars"
)

// session is the variable state and model snapshot shared by every request of
// one Send or one Run. A single vars.MemStore lives for the whole session, so
// a variable a script sets in request 1 of a run is visible to request 2, and
// its persistence is tracked incrementally (applied counts the changes already
// written).
type session struct {
	e  *Engine
	id uint64

	// collection, env and globals are deep copies taken when the session
	// started, with secret variable values hydrated. Nothing outside the
	// session can mutate them.
	collection *model.Collection
	env        *model.Environment
	globals    *model.Globals

	store   *vars.MemStore
	applied map[vars.Scope]int
	secrets map[string]struct{}
	// warnings raised while building the session; delivered with the first
	// outcome.
	warnings       []string
	iterationCount int
	// noPersist skips disk writes for variable changes (collection runs).
	noPersist bool
}

// newSession snapshots the models, hydrates their secret variables and builds
// the variable store.
func (e *Engine) newSession(
	ctx context.Context,
	collection *model.Collection,
	environment *model.Environment,
	globals *model.Globals,
	data map[string]any,
) (*session, error) {
	if collection == nil {
		return nil, fmt.Errorf("%w: no collection", ErrInvalidInput)
	}
	s := &session{
		e:              e,
		id:             e.nextID.Add(1),
		collection:     collection.Clone(),
		env:            environment.Clone(),
		applied:        map[vars.Scope]int{},
		secrets:        map[string]struct{}{},
		iterationCount: 1,
	}
	if globals != nil {
		s.globals = globals.Clone()
	} else {
		g, err := e.loadGlobals()
		if err != nil {
			return nil, fmt.Errorf("read globals: %w", err)
		}
		s.globals = g
	}

	missing := 0
	hydrate := func(list []model.Variable) error {
		m, err := e.cfg.Store.HydrateVariables(ctx, list)
		missing += len(m)
		return err
	}
	if err := hydrate(s.collection.Variables); err != nil {
		return nil, fmt.Errorf("read secret variables: %w", err)
	}
	var hydrateErr error
	_ = s.collection.Walk(func(it model.Item, _ model.Container, _ int) error {
		if folder, ok := it.(*model.Folder); ok && hydrateErr == nil {
			hydrateErr = hydrate(folder.Variables)
		}
		return nil
	})
	if hydrateErr != nil {
		return nil, fmt.Errorf("read secret variables: %w", hydrateErr)
	}
	var envVars, globalVars []model.Variable
	if s.env != nil {
		if err := hydrate(s.env.Variables); err != nil {
			return nil, fmt.Errorf("read secret variables: %w", err)
		}
		envVars = s.env.Variables
	}
	if s.globals != nil {
		if err := hydrate(s.globals.Variables); err != nil {
			return nil, fmt.Errorf("read secret variables: %w", err)
		}
		globalVars = s.globals.Variables
	}
	if missing > 0 {
		s.warnings = append(s.warnings, fmt.Sprintf(
			"%d secret variable(s) have no stored value; set them in the environment editor", missing))
	}
	s.store = vars.New(globalVars, s.collection.Variables, envVars, data)
	s.refreshSecrets()
	return s, nil
}

// refreshSecrets records every secret variable value currently held, so the
// history redactor knows them (including values a script just wrote).
func (s *session) refreshSecrets() {
	add := func(list []model.Variable) {
		for _, v := range list {
			if v.IsSecret() && v.Value != "" {
				s.secrets[v.Value] = struct{}{}
			}
		}
	}
	add(s.collection.Variables)
	_ = s.collection.Walk(func(it model.Item, _ model.Container, _ int) error {
		if folder, ok := it.(*model.Folder); ok {
			add(folder.Variables)
		}
		return nil
	})
	if s.env != nil {
		add(s.env.Variables)
	}
	if s.globals != nil {
		add(s.globals.Variables)
	}
}

func (s *session) addAuthSecrets(auth *model.Auth) {
	if auth == nil || auth.OAuth2 == nil || auth.OAuth2.Token == nil {
		return
	}
	tok := auth.OAuth2.Token
	for _, value := range []string{tok.AccessToken, tok.RefreshToken, tok.IDToken} {
		if value != "" {
			s.secrets[value] = struct{}{}
		}
	}
}

func (s *session) redactor(extraNames ...string) *history.Redactor {
	values := make([]string, 0, len(s.secrets))
	for v := range s.secrets {
		values = append(values, v)
	}
	sort.Strings(values)
	return history.NewRedactor(values, extraNames...)
}

// chainFor returns the containers above request id, outermost first: the
// collection, then each folder. A request that is not in the collection
// (a new unsaved one) sits directly under it.
func (s *session) chainFor(id model.ID) []model.Container {
	chain := []model.Container{s.collection}
	_, parent, _, ok := s.collection.Find(id)
	if !ok {
		return chain
	}
	var folders []model.Container
	for parent != nil {
		folder, isFolder := parent.(*model.Folder)
		if !isFolder {
			break
		}
		folders = append(folders, folder)
		_, parent, _, ok = s.collection.Find(folder.ID)
		if !ok {
			break
		}
	}
	for i := len(folders) - 1; i >= 0; i-- {
		chain = append(chain, folders[i])
	}
	return chain
}

func pathNames(chain []model.Container, requestName string) []string {
	names := make([]string, 0, len(chain)+1)
	for _, c := range chain {
		switch n := c.(type) {
		case *model.Collection:
			names = append(names, n.Name)
		case *model.Folder:
			names = append(names, n.Name)
		}
	}
	return append(names, requestName)
}

type levelScript struct {
	origin string
	script *model.Script
	step   string
}

// scriptsFor lists the runnable scripts of one event: collection, folders
// outermost first, then the request.
func scriptsFor(chain []model.Container, req *model.Request, event script.Event) []levelScript {
	var out []levelScript
	pick := func(sc model.Scripts) *model.Script {
		if event == script.EventPreRequest {
			return sc.PreRequest
		}
		return sc.Test
	}
	add := func(origin string, sc *model.Script, step string) {
		if sc != nil && !sc.Disabled && strings.TrimSpace(sc.Source) != "" {
			out = append(out, levelScript{origin: origin, script: sc, step: step})
		}
	}
	collStep, folderStep, reqStep := stepCollectionTests, stepFolderTests, stepRequestTests
	if event == script.EventPreRequest {
		collStep, folderStep, reqStep = stepCollectionPre, stepFolderPre, stepRequestPre
	}
	for _, c := range chain {
		switch n := c.(type) {
		case *model.Collection:
			add("collection", pick(n.Scripts), collStep)
		case *model.Folder:
			add("folder", pick(n.Scripts), folderStep)
		}
	}
	add("request", pick(req.Scripts), reqStep)
	return out
}

// unresolvedRef finds {{name}} references left after resolution.
var unresolvedRef = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

// execute runs the whole pipeline of plan §4.1 for one request and never
// panics or returns an error: everything that goes wrong is recorded on the
// Outcome.
func (s *session) execute(
	ctx context.Context,
	req *model.Request,
	chain []model.Container,
	iteration int,
	progress func(Step),
) *Outcome {
	emit := func(kind StepKind, text string) {
		if progress != nil {
			progress(Step{Kind: kind, Text: text})
		}
	}
	e := s.e
	started := e.cfg.Clock()
	out := &Outcome{
		RequestID:   req.ID,
		RequestName: req.Name,
		Path:        pathNames(chain, req.Name),
		Iteration:   iteration,
		Started:     started,
		Warnings:    s.warnings,
	}
	s.warnings = nil
	defer func() { out.Duration = e.cfg.Clock().Sub(started) }()

	if err := ctx.Err(); err != nil {
		out.Err = err
		return out
	}

	info := script.Info{
		RequestName:    req.Name,
		RequestID:      string(req.ID),
		CollectionName: s.collection.Name,
		Iteration:      iteration,
		IterationCount: s.iterationCount,
	}
	// Folder variables depend on where the request sits, so they are
	// installed per request, before any script or resolution runs.
	s.store.SetFolderVariables(vars.FolderVariables(chain))
	opts := e.httpOptions()
	sender := &varSender{inner: e.cfg.HTTP, store: s.store}

	// Pre-request scripts see the request with its {{variables}} unresolved.
	working := req.Clone()
	for _, level := range scriptsFor(chain, req, script.EventPreRequest) {
		emit(StepPreRequest, level.step)
		res, err := script.Run(ctx, []*model.Script{level.script}, script.Input{
			Event:       script.EventPreRequest,
			Request:     working,
			Vars:        s.store,
			Info:        info,
			Sender:      sender,
			SendOptions: opts,
			Cookies:     e.cookies,
			Timeout:     e.cfg.ScriptTimeout,
			MemoryLimit: e.cfg.ScriptMemoryLimit,
		})
		if err != nil {
			out.Err = err
			s.finish(ctx, out, progress)
			return out
		}
		working = res.Request
		s.collect(out, res, level.origin, script.EventPreRequest)
		if ctx.Err() != nil {
			out.Err = ctx.Err()
			s.finish(ctx, out, progress)
			return out
		}
		if res.Err != nil {
			out.ScriptError = failure(script.EventPreRequest, level.origin, res)
			s.finish(ctx, out, progress)
			return out
		}
		if res.SkipRequest {
			out.Skipped = true
			s.finish(ctx, out, progress)
			return out
		}
	}

	emit(StepResolve, stepResolve)
	resolved, err := vars.ResolveRequest(working, chain, s.store)
	if err != nil {
		out.Err = err
		s.finish(ctx, out, progress)
		return out
	}
	if resolved.Kind == "" {
		resolved.Kind = model.KindHTTP
	}
	out.Resolved = resolved
	out.Warnings = append(out.Warnings, unresolvedWarnings(resolved.URL.Raw)...)
	s.addAuthSecrets(resolved.Auth)

	emit(StepSend, stepSend)
	resp, err := e.cfg.HTTP.Send(ctx, resolved, opts)
	s.addAuthSecrets(resolved.Auth)
	redactor := s.redactor(authNames(resolved.Auth)...)
	if err != nil {
		out.Err = sendError(ctx, err, redactor)
		s.finish(ctx, out, progress)
		return out
	}
	out.Response = resp

	for _, level := range scriptsFor(chain, req, script.EventTest) {
		emit(StepTest, level.step)
		res, err := script.Run(ctx, []*model.Script{level.script}, script.Input{
			Event:       script.EventTest,
			Request:     resolved,
			Response:    resp,
			Vars:        s.store,
			Info:        info,
			Sender:      sender,
			SendOptions: opts,
			Cookies:     e.cookies,
			Timeout:     e.cfg.ScriptTimeout,
			MemoryLimit: e.cfg.ScriptMemoryLimit,
		})
		if err != nil {
			out.Err = err
			break
		}
		s.collect(out, res, level.origin, script.EventTest)
		if ctx.Err() != nil {
			out.Err = ctx.Err()
			break
		}
		if res.Err != nil {
			// A failing test script does not undo the exchange: the
			// response, the tests recorded so far and the variables
			// are all kept.
			out.ScriptError = failure(script.EventTest, level.origin, res)
			break
		}
	}
	s.finish(ctx, out, progress)
	return out
}

// finish persists variable changes and records history, the two steps every
// path through the pipeline ends with.
func (s *session) finish(ctx context.Context, out *Outcome, progress func(Step)) {
	saved, warnings := s.persist(ctx, progress)
	out.Persisted = saved
	out.Warnings = append(out.Warnings, warnings...)
	s.refreshSecrets()
	if out.Resolved != nil {
		s.addAuthSecrets(out.Resolved.Auth)
		out.Redactor = s.redactor(authNames(out.Resolved.Auth)...)
	} else {
		out.Redactor = s.redactor()
	}
	s.record(ctx, out)
}

// collect folds one script run into the outcome.
func (s *session) collect(out *Outcome, res *script.Result, origin string, event script.Event) {
	for _, t := range res.Tests {
		out.Tests = append(out.Tests, TestResult{
			Name:    t.Name,
			Passed:  t.Passed,
			Skipped: t.Skipped,
			Message: t.Error,
			Origin:  origin,
		})
	}
	for _, c := range res.Console {
		out.Console = append(out.Console, ConsoleLine{
			Level: string(c.Level),
			Text:  c.Message,
			Event: string(event),
			Time:  c.Time,
		})
	}
	if res.NextRequest != nil {
		next := *res.NextRequest
		out.NextRequest = &next
	}
}

func failure(event script.Event, origin string, res *script.Result) *ScriptFailure {
	return &ScriptFailure{
		Event:    string(event),
		Origin:   origin,
		Text:     res.Err.Error(),
		TimedOut: res.TimedOut,
		Err:      res.Err,
	}
}

func unresolvedWarnings(rawURL string) []string {
	seen := map[string]bool{}
	var names []string
	for _, m := range unresolvedRef.FindAllStringSubmatch(rawURL, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			names = append(names, m[1])
		}
	}
	warnings := make([]string, 0, len(names))
	for _, n := range names {
		warnings = append(warnings, fmt.Sprintf("Unresolved variable {{%s}} in the URL", n))
	}
	return warnings
}

// authNames returns the API-key name of a resolved auth, so the history
// redactor treats it as a credential even when it looks harmless.
func authNames(auth *model.Auth) []string {
	if auth != nil && auth.APIKey != nil && auth.APIKey.Key != "" {
		return []string{auth.APIKey.Key}
	}
	return nil
}

// sendError turns a transport failure into the error shown to the user. The
// URL inside a *url.Error is redacted, because Go includes it (query string
// and all) in the message.
func sendError(ctx context.Context, err error, r *history.Redactor) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errors.Is(err, oauth2.ErrNoToken) {
		return &OAuthTokenMissingError{Err: err}
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return &url.Error{Op: ue.Op, URL: r.URL(ue.URL), Err: ue.Err}
	}
	return err
}

// varSender is the Sender behind pm.sendRequest. It resolves {{variables}}
// from the live store (so a script can send to {{baseUrl}}) and uses the
// options the script runtime hands it, which carry the workspace cookie jar.
// pm.sendRequest requests never run scripts and never enter history.
type varSender struct {
	inner httpx.Sender
	store vars.Store
}

// Send resolves req against the live variable store and sends it.
func (v *varSender) Send(ctx context.Context, req *model.Request, opts httpx.Options) (*httpx.Response, error) {
	resolved, err := vars.ResolveRequest(req, nil, v.store)
	if err != nil {
		return nil, err
	}
	if resolved.Kind == "" {
		resolved.Kind = model.KindHTTP
	}
	return v.inner.Send(ctx, resolved, opts)
}
