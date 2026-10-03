package complete

import (
	"regexp"
	"sort"
	"strings"

	"github.com/shubham/postctl/internal/tui/vimtext"
)

// The pm.* member table. It is the one place that lists what a script can
// reach; it follows the compatibility table in docs/scripting.md and
// TestPMTableCoversScriptingDoc fails when the document names a member that is
// missing here.
//
// Each value lists the members of an object, separated by spaces. A trailing
// "." marks a member that is itself an object (the table has an entry for its
// path), a trailing "(" marks a function, and a bare name is a plain property.

// scopeMembers are shared by every variable scope object.
const scopeMembers = "get( set( unset( has( clear( toObject( replaceIn( variables syncVariablesTo( syncVariablesFrom("

// listMembers are the PropertyList methods behind query, variables, headers and cookies.
const listMembers = "add( insert( append( prepend( upsert( remove( get( one( has( each( map( filter( find( reduce( count( idx( indexOf( clear( populate( repopulate( assimilate( toObject( toJSON("

var pmTable = map[string]string{
	"pm": "environment. collectionVariables. globals. iterationData. variables. request. response. " +
		"test( expect( cookies. info. execution. sendRequest( visualizer. vault. require(",

	"pm.environment":         scopeMembers,
	"pm.collectionVariables": scopeMembers,
	"pm.globals":             scopeMembers,
	"pm.iterationData":       scopeMembers,
	"pm.variables":           scopeMembers,

	"pm.request": "method name id url. headers. body. auth. addHeader( upsertHeader( removeHeader( getHeaders( clone( size(",
	"pm.request.url": "toString( getHost( getPath( getPathWithQuery( getQueryString( getRemote( addQueryParams( " +
		"removeQueryParams( toJSON( query. variables. path.",
	"pm.request.url.path":      "push( splice( pop( shift( unshift( slice( join( length",
	"pm.request.url.query":     listMembers,
	"pm.request.url.variables": listMembers,
	"pm.request.headers":       listMembers,
	"pm.request.body":          "mode raw urlencoded formdata file graphql isEmpty( toString(",
	"pm.request.auth":          "update( use( parameters( current( clear(",

	"pm.response": "code status responseTime responseSize text( json( headers. cookies. reason( details( size( " +
		"dataURI( jsonp( contentInfo( to.",
	"pm.response.headers": listMembers,
	"pm.response.cookies": listMembers,
	"pm.response.to":      "have. be.",
	"pm.response.to.have": "status( header( body( jsonBody( jsonSchema(",
	"pm.response.to.be": "ok success error clientError serverError redirection accepted badRequest unauthorized " +
		"forbidden notFound rateLimited withBody json info",

	"pm.test":               "skip( index(",
	"pm.cookies":            "get( one( has( count each( toObject( jar(",
	"pm.cookies.jar":        "get( getAll( set( unset( clear(",
	"pm.info":               "eventName iteration iterationCount requestName requestId collectionName",
	"pm.execution":          "setNextRequest( skipRequest( location. runRequest(",
	"pm.execution.location": "current",
	"pm.visualizer":         "set(",
	"pm.vault":              "get( set( unset(",
}

// pmExpectSubjects are offered right after "pm.expect(".
var pmExpectSubjects = []string{
	"pm.response.code", "pm.response.json()", "pm.response.text()", "pm.response.responseTime",
	"pm.response.responseSize", "pm.response.headers.get(", "pm.variables.get(", "pm.environment.get(",
}

// chaiWords are the chai chain members offered after pm.expect(...).
const chaiWords = "to. be. been. is. that. which. and. has. have. with. at. of. same. but. does. still. not. deep. " +
	"nested. own. ordered. any. all. a( an( include( includes( contain( contains( equal( equals( eq( eql( eqls( " +
	"above( gt( greaterThan( least( gte( below( lt( lessThan( most( lte( within( instanceof( property( " +
	"ownProperty( lengthOf( length match( string( keys( key( throw( respondTo( satisfy( closeTo( members( " +
	"oneOf( change( increase( decrease( ok true false null undefined exist empty arguments NaN finite"

// pmMember is one parsed table entry.
type pmMember struct {
	name   string
	object bool
	call   bool
}

func parseMembers(spec string) []pmMember {
	var out []pmMember
	for _, field := range strings.Fields(spec) {
		m := pmMember{name: field}
		switch {
		case strings.HasSuffix(field, "."):
			m.name, m.object = strings.TrimSuffix(field, "."), true
		case strings.HasSuffix(field, "("):
			m.name, m.call = strings.TrimSuffix(field, "("), true
		}
		out = append(out, m)
	}
	return out
}

// PMMembers returns the members the table lists for an object path such as
// "pm.environment", or nil when the path is not in the table. Names are bare:
// no trailing "." or "(".
func PMMembers(path string) []string {
	spec, ok := pmTable[path]
	if !ok {
		return nil
	}
	var out []string
	for _, m := range parseMembers(spec) {
		out = append(out, m.name)
	}
	return out
}

// PMPaths returns every object path in the table, sorted.
func PMPaths() []string {
	out := make([]string, 0, len(pmTable))
	for path := range pmTable {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

type pmCompleter struct{}

// PM returns the completer for pm.* members in scripts: after "pm." and deeper
// ("pm.environment.", "pm.response.to.have.", "pm.cookies.jar().") it lists
// the members of that object; after "pm.expect(" it offers common subjects,
// and after "pm.expect(...)." the chai chain words.
func PM() vimtext.Completer { return pmCompleter{} }

var pmTail = regexp.MustCompile(`(?:^|[^A-Za-z0-9_$.])(pm(?:\.[A-Za-z_][A-Za-z0-9_]*(?:\(\))?)*)\.([A-Za-z_]*)$`)

// Complete implements [vimtext.Completer].
func (pmCompleter) Complete(before string) ([]vimtext.Item, int) {
	if strings.HasSuffix(before, "pm.expect(") {
		items := make([]vimtext.Item, len(pmExpectSubjects))
		for i, s := range pmExpectSubjects {
			items[i] = vimtext.Item{Label: s, Detail: "subject", Insert: s}
		}
		return items, 0
	}
	if m := pmTail.FindStringSubmatch(before); m != nil {
		prefix := m[2]
		if spec, ok := pmTable[strings.ReplaceAll(m[1], "()", "")]; ok {
			return membersOf(spec, prefix), len(prefix)
		}
		return nil, 0
	}
	if items, replace, ok := chaiChain(before); ok {
		return items, replace
	}
	return nil, 0
}

// membersOf turns a table entry into candidates whose name starts with
// prefix, or contains it when nothing starts with it.
func membersOf(spec, prefix string) []vimtext.Item {
	lower := strings.ToLower(prefix)
	var starts, contains []vimtext.Item
	for _, m := range parseMembers(spec) {
		name := strings.ToLower(m.name)
		item := vimtext.Item{Label: m.name, Insert: m.name}
		switch {
		case m.object:
			item.Detail, item.Insert = "object", m.name+"."
		case m.call:
			item.Detail, item.Insert = "function", m.name+"("
		default:
			item.Detail = "property"
		}
		switch {
		case strings.HasPrefix(name, lower):
			starts = append(starts, item)
		case strings.Contains(name, lower):
			contains = append(contains, item)
		}
	}
	if len(starts) > 0 {
		return starts
	}
	return contains
}

// chaiChain completes the assertion chain after a closed pm.expect(...) call.
func chaiChain(before string) ([]vimtext.Item, int, bool) {
	i := strings.LastIndex(before, "pm.expect(")
	if i < 0 {
		return nil, 0, false
	}
	rest := before[i+len("pm.expect("):]
	depth := 1
	closed := -1
	for j, r := range rest {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && closed < 0 {
				closed = j
			}
		}
	}
	if closed < 0 {
		return nil, 0, false
	}
	tail := rest[closed+1:]
	dot := strings.LastIndex(tail, ".")
	if dot < 0 {
		return nil, 0, false
	}
	prefix := tail[dot+1:]
	for _, r := range prefix {
		if r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return nil, 0, false
		}
	}
	items := membersOf(chaiWords, prefix)
	return items, len(prefix), len(items) > 0
}
