package postman

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
)

// Synthetic credentials: every one must stay out of the YAML files.
const (
	synSessionToken = "synthetic-sts-session-token-0001"
	synAWSSecret    = "synthetic-aws-secret-0002"
	synClientSecret = "synthetic-client-secret-0003"
	synVerifier     = "synthetic-code-verifier-0004"
	synAccess       = "synthetic-access-token-0005"
	synRefresh      = "synthetic-refresh-token-0006"
	synConsumer     = "synthetic-oauth1-consumer-secret-0007"
	synTokenSecret  = "synthetic-oauth1-token-secret-0008"
	synJWTSecret    = "synthetic-jwt-secret-0009"
	synPrivateKey   = "synthetic-asap-private-key-0010"
	synHawk         = "synthetic-hawk-key-0011"
	synExtra        = "synthetic-extra-param-0012"
)

var secretCollection = `{
 "info":{"_postman_id":"c-secrets","name":"Secrets","schema":"` + schemaV21 + `"},
 "item":[
  {"id":"aws","name":"AWS","request":{"method":"GET","url":"https://example.invalid/a","auth":{"type":"awsv4","awsv4":[
    {"key":"accessKey","value":"AKIASYNTHETIC"},{"key":"secretKey","value":"` + synAWSSecret + `"},
    {"key":"sessionToken","value":"` + synSessionToken + `"},{"key":"region","value":"us-east-1"}]}}},
  {"id":"oauth","name":"OAuth","request":{"method":"GET","url":"https://example.invalid/o","auth":{"type":"oauth2","oauth2":[
    {"key":"grant_type","value":"authorization_code_with_pkce"},{"key":"clientSecret","value":"` + synClientSecret + `"},
    {"key":"codeVerifier","value":"` + synVerifier + `"},{"key":"accessToken","value":"` + synAccess + `"},
    {"key":"refreshToken","value":"` + synRefresh + `"},{"key":"tokenType","value":"Bearer"},
    {"key":"vendorSecret","value":"` + synExtra + `"},{"key":"addTokenTo","value":"header"}]}}},
  {"id":"o1","name":"OAuth1","request":{"method":"GET","url":"https://example.invalid/1","auth":{"type":"oauth1","oauth1":[
    {"key":"consumerKey","value":"public-key"},{"key":"consumerSecret","value":"` + synConsumer + `"},
    {"key":"tokenSecret","value":"` + synTokenSecret + `"},{"key":"signatureMethod","value":"HMAC-SHA1"}]}}},
  {"id":"jwt","name":"JWT","request":{"method":"GET","url":"https://example.invalid/j","auth":{"type":"jwt","jwt":[
    {"key":"secret","value":"` + synJWTSecret + `"},{"key":"algorithm","value":"HS256"}]}}},
  {"id":"asap","name":"ASAP","request":{"method":"GET","url":"https://example.invalid/s","auth":{"type":"asap","asap":[
    {"key":"privateKey","value":"` + synPrivateKey + `"},{"key":"iss","value":"issuer"}]}}},
  {"id":"hawk","name":"Hawk","request":{"method":"GET","url":"https://example.invalid/h","auth":{"type":"hawk","hawk":[
    {"key":"authId","value":"id"},{"key":"authKey","value":"` + synHawk + `"}]}}},
  {"id":"ref","name":"Ref","request":{"method":"GET","url":"https://example.invalid/r","auth":{"type":"awsv4","awsv4":[
    {"key":"secretKey","value":"{{already_a_variable}}"},{"key":"sessionToken","value":"{{sts}}"}]}}}
 ]}`

var allSecrets = []string{
	synSessionToken, synAWSSecret, synClientSecret, synVerifier, synAccess, synRefresh,
	synConsumer, synTokenSecret, synJWTSecret, synPrivateKey, synHawk, synExtra,
}

func yamlText(t *testing.T, root string) string {
	t.Helper()
	var all strings.Builder
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		all.Write(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return all.String()
}

func TestImporterMovesEveryAuthSecret(t *testing.T) {
	document, report, err := Convert([]byte(secretCollection), Options{})
	if err != nil {
		t.Fatal(err)
	}
	collection := document.Collection

	// AWS: secret key and session token both become secret variables.
	aws := requestByID(t, collection, "aws").Auth.AWSV4
	if aws.SessionToken != "{{awsv4_session_token}}" || aws.SecretKey != "{{awsv4_secret_key}}" {
		t.Fatalf("aws auth not rewritten: session=%q secret=%q", aws.SessionToken, aws.SecretKey)
	}
	if aws.AccessKey != "AKIASYNTHETIC" {
		t.Fatal("the access key ID is an identifier and must be left alone")
	}
	if v, ok := model.Lookup(collection.Variables, "awsv4_session_token"); !ok || v.Type != model.VarSecret || v.Value != synSessionToken {
		t.Fatalf("session token variable = %+v", v)
	}
	// Already-parameterised fields are left as they are.
	ref := requestByID(t, collection, "ref").Auth.AWSV4
	if ref.SecretKey != "{{already_a_variable}}" || ref.SessionToken != "{{sts}}" {
		t.Fatal("variable references must not be moved")
	}

	oauth := requestByID(t, collection, "oauth").Auth.OAuth2
	if oauth.ClientSecret != "{{oauth2_client_secret}}" || oauth.CodeVerifier != "{{oauth2_code_verifier}}" {
		t.Fatalf("oauth2 client secret / code verifier not moved: %q %q", oauth.ClientSecret, oauth.CodeVerifier)
	}
	if oauth.Extra["vendorSecret"] != "{{oauth2_vendorsecret}}" {
		t.Fatalf("credential-like extra oauth2 parameter not moved: %v", oauth.Extra["vendorSecret"])
	}
	if oauth.Extra["addTokenTo"] != nil || oauth.AddTokenTo != model.TokenInHeader {
		t.Fatal("addTokenTo is a setting, not a secret")
	}

	// Unsupported schemes keep their parameters in Raw; credentials leave it.
	o1 := requestByID(t, collection, "o1").Auth.Raw["parameters"].(map[string]any)
	if o1["consumerSecret"] == synConsumer || o1["tokenSecret"] == synTokenSecret ||
		!strings.HasPrefix(o1["consumerSecret"].(string), "{{") {
		t.Fatalf("oauth1 secrets not moved: %v", o1)
	}
	if o1["consumerKey"] != "public-key" || o1["signatureMethod"] != "HMAC-SHA1" {
		t.Fatal("non-secret oauth1 parameters must be untouched")
	}
	if requestByID(t, collection, "jwt").Auth.Raw["parameters"].(map[string]any)["secret"] == synJWTSecret {
		t.Fatal("jwt secret not moved")
	}
	if requestByID(t, collection, "asap").Auth.Raw["parameters"].(map[string]any)["privateKey"] == synPrivateKey {
		t.Fatal("asap private key not moved")
	}
	if requestByID(t, collection, "hawk").Auth.Hawk.Key != "{{hawk_key}}" {
		t.Fatal("hawk key not moved")
	}

	// Counts are truthful: one per variable created, and none for OAuth tokens.
	secretVars := 0
	for _, v := range collection.Variables {
		if v.IsSecret() {
			secretVars++
		}
	}
	if report.Counts.SecretsMoved != secretVars {
		t.Fatalf("SecretsMoved = %d but %d secret variables were created", report.Counts.SecretsMoved, secretVars)
	}
	if report.Counts.OAuthTokens != 2 {
		t.Fatalf("OAuthTokens = %d, want 2 (access and refresh)", report.Counts.OAuthTokens)
	}
}

func TestImportedWorkspaceHoldsNoSecretsInFiles(t *testing.T) {
	document, _, err := Convert([]byte(secretCollection), Options{})
	if err != nil {
		t.Fatal(err)
	}
	workspace := &model.Workspace{ID: "w", Name: "W"}
	MergeDocuments(workspace, []*Document{document}, &Report{})
	root := t.TempDir()
	memory := store.NewMemorySecrets()
	st, err := store.Init(root, "W", store.WithSecretStore(memory))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	text := yamlText(t, root)
	for _, secret := range allSecrets {
		if strings.Contains(text, secret) {
			t.Fatalf("a synthetic credential (%s...) was written to the workspace files", secret[:20])
		}
	}
	// The values are not lost: the Keychain (memory fake) holds them all.
	held := 0
	for _, ref := range memory.Refs() {
		value, _ := memory.Get(context.Background(), ref)
		for _, secret := range allSecrets {
			if strings.Contains(value, secret) {
				held++
			}
		}
	}
	if held < len(allSecrets) {
		t.Fatalf("secret store holds %d of %d credentials", held, len(allSecrets))
	}
}

func TestKeepLiteralSecretsKeepsAuthParams(t *testing.T) {
	document, report, err := Convert([]byte(secretCollection), Options{KeepLiteralSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts.SecretsMoved != 0 || len(document.Collection.Variables) != 0 {
		t.Fatalf("keep-literal-secrets moved something: %+v", report.Counts)
	}
	if got := requestByID(t, document.Collection, "aws").Auth.AWSV4.SessionToken; got != synSessionToken {
		t.Fatal("keep-literal-secrets must leave the session token in place")
	}
	// OAuth tokens still reach the Keychain when the store saves them, so the
	// count stays truthful even with the opt-out.
	if report.Counts.OAuthTokens != 2 {
		t.Fatalf("OAuthTokens = %d", report.Counts.OAuthTokens)
	}
}

func TestReportWordsDryRunHonestly(t *testing.T) {
	report := Report{DryRun: true, Counts: Counts{SecretsMoved: 2, OAuthTokens: 1}}
	var out bytes.Buffer
	report.WriteText(&out)
	if !strings.Contains(out.String(), "would move") || strings.Contains(out.String(), "moved to") {
		t.Fatalf("dry run report claims a move:\n%s", out.String())
	}
	report.DryRun = false
	out.Reset()
	report.WriteText(&out)
	if !strings.Contains(out.String(), "2 moved to Keychain variables") {
		t.Fatalf("report:\n%s", out.String())
	}
}

func requestByID(t *testing.T, c *model.Collection, id string) *model.Request {
	t.Helper()
	item, _, _, ok := c.Find(model.ID(id))
	request, isRequest := item.(*model.Request)
	if !ok || !isRequest {
		t.Fatalf("request %q missing", id)
	}
	return request
}
