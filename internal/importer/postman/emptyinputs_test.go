package postman

import (
	"strings"
	"testing"
)

const emptyInputsCollection = `{
 "info":{"_postman_id":"c-empty","name":"Inputs","schema":"` + schemaV21 + `"},
 "variable":[{"key":"collectionEmpty","value":""},{"key":"collectionFilled","value":"x"}],
 "auth":{"type":"bearer","bearer":[{"key":"token","value":"{{auth_token}}"}]},
 "event":[{"listen":"prerequest","script":{"exec":[
   "pm.environment.set('krakenD_token', 'synthetic-' + 'value');"]}}],
 "item":[
  {"id":"f1","name":"Folder","variable":[{"key":"folderFilled","value":"y"}],"item":[
   {"id":"r1","name":"Uses token","event":[{"listen":"prerequest","script":{"exec":[
      "const t = pm.environment.get('krakenD_token');",
      "const c = pm.variables.get(\"CF_COOKIE_HEADER\");",
      "const s = pm.environment.get('seedValue'); pm.environment.set('seedValue', 'a');",
      "pm.collectionVariables.get('collectionEmpty');",
      "pm.environment.get('filledElsewhere'); pm.environment.get('folderFilled');",
      "pm.environment.get(dynamicKey);"]}}],
    "request":{"method":"GET","url":{"raw":"https://{{host}}/x?b={{queryEmpty}}","query":[
        {"key":"a","value":"{{disabledQuery}}","disabled":true},{"key":"b","value":"{{queryEmpty}}"}]},
      "header":[{"key":"X-A","value":"{{headerEmpty}}"},{"key":"X-B","value":"{{disabledHeader}}","disabled":true},
                {"key":"X-C","value":"{{$guid}} {{ spacedEmpty }}"}],
      "body":{"mode":"raw","raw":"{\"k\":\"{{bodyEmpty}}\"}"}}}
  ]}
 ]}`

func envDoc(name string, values ...string) string {
	// values are "key=value" pairs
	var entries []string
	for _, pair := range values {
		key, value, _ := strings.Cut(pair, "=")
		entries = append(entries, `{"key":"`+key+`","value":"`+value+`","type":"default","enabled":true}`)
	}
	return `{"id":"env-` + name + `","name":"` + name + `","_postman_variable_scope":"environment","values":[` +
		strings.Join(entries, ",") + `]}`
}

func TestCheckEmptyInputs(t *testing.T) {
	declared := []string{
		"auth_token=", "krakenD_token=", "CF_COOKIE_HEADER=", "seedValue=", "filledElsewhere=",
		"folderFilled=", "queryEmpty=", "headerEmpty=", "disabledHeader=", "disabledQuery=", "spacedEmpty=",
		"bodyEmpty=", "unreadEmpty=", "host=https://example.invalid", "notAnInput=",
	}
	preview, _ := convertNamed(t, envDoc("Preview", declared...), Options{})
	production, _ := convertNamed(t, envDoc("Production", append(append([]string{}, declared...), "filledElsewhere=has-a-value")...), Options{})
	spaced, _ := convertNamed(t, envDoc("My Env", "auth_token="), Options{})
	collection, _ := convertNamed(t, emptyInputsCollection, Options{})

	report := Report{}
	CheckEmptyInputs([]*Document{preview, production, spaced, collection}, &report)

	var got []string
	for _, warning := range report.Warnings {
		if warning.Code != "empty-input" {
			t.Errorf("unexpected warning code %q", warning.Code)
		}
		got = append(got, warning.Message)
	}
	joined := strings.Join(got, "\n")

	want := []string{
		"auth_token is empty in My Env; set it with `postctl env set 'My Env' auth_token --secret`",
		"collectionEmpty is empty in collection Inputs; give it a value in the collection's variables",
	}
	for _, env := range []string{"Preview", "Production"} {
		for _, key := range []string{"auth_token", "CF_COOKIE_HEADER"} {
			want = append(want, key+" is empty in "+env+"; set it with `postctl env set "+env+" "+key+" --secret`")
		}
		for _, key := range []string{"queryEmpty", "headerEmpty", "spacedEmpty", "bodyEmpty"} {
			want = append(want, key+" is empty in "+env+"; set it with `postctl env set "+env+" "+key+" <value>`")
		}
	}
	for _, line := range want {
		if !strings.Contains(joined, line) {
			t.Errorf("missing warning %q\ngot:\n%s", line, joined)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d warnings, want %d:\n%s", len(got), len(want), joined)
	}
	for _, key := range []string{
		"krakenD_token",                   // a script sets it
		"seedValue",                       // read, but a script sets it
		"filledElsewhere", "folderFilled", // valued in an environment or a folder
		"disabledHeader", "disabledQuery", // only in disabled request parts
		"unreadEmpty", "notAnInput", // never read
		"host", "collectionFilled",
	} {
		if strings.Contains(joined, key+" is empty") {
			t.Errorf("%s should not be warned about:\n%s", key, joined)
		}
	}
}

func TestCheckEmptyInputsIgnoresUndeclaredAndDisabledScripts(t *testing.T) {
	doc := `{"info":{"_postman_id":"c","name":"C","schema":"` + schemaV21 + `"},"item":[
	 {"id":"r","name":"R","event":[{"listen":"prerequest","disabled":true,"script":{"exec":["pm.environment.set('x','1');"]}},
	   {"listen":"test","script":{"exec":["pm.environment.get('x');"]}}],
	  "request":{"method":"GET","url":"https://example.invalid/{{undeclared}}"}}]}`
	collection, _ := convertNamed(t, doc, Options{})
	env, _ := convertNamed(t, envDoc("E", "x="), Options{})
	report := Report{}
	CheckEmptyInputs([]*Document{env, collection}, &report)
	// x is read and its only setter is a disabled script, so it is reported;
	// "undeclared" is not declared anywhere, so it is not an empty input.
	if len(report.Warnings) != 1 || !strings.HasPrefix(report.Warnings[0].Message, "x is empty in E;") {
		t.Fatalf("warnings = %+v", report.Warnings)
	}
}

func TestEmptyInputWarningsNeverContainValues(t *testing.T) {
	env, _ := convertNamed(t, envDoc("E", "auth_token=", "other=synthetic-value-should-not-appear"), Options{})
	collection, _ := convertNamed(t, `{"info":{"_postman_id":"c","name":"C","schema":"`+schemaV21+`"},"item":[
	 {"id":"r","name":"R","request":{"method":"GET","url":"https://example.invalid/{{auth_token}}/{{other}}"}}]}`, Options{})
	report := Report{}
	CheckEmptyInputs([]*Document{env, collection}, &report)
	var text strings.Builder
	report.WriteText(&text)
	if strings.Contains(text.String(), "synthetic-value-should-not-appear") {
		t.Fatal("the report printed a variable value")
	}
	if !strings.Contains(text.String(), "auth_token is empty in E;") {
		t.Fatalf("warning missing from the text report:\n%s", text.String())
	}
}
