package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/store"
)

// envSetWith runs `env set` with the given stdin. tty selects the prompt
// path; readPassword stands in for the terminal read.
func envSetWith(st *store.Store, stdin string, tty bool, readPassword func() ([]byte, error), args ...string) (code int, out, errOut string) {
	var stdout, stderr bytes.Buffer
	opts := options(st, &stdout, &stderr)
	opts.Stdin = strings.NewReader(stdin)
	opts.IsTTY = func(any) bool { return tty }
	opts.ReadPassword = readPassword
	code = Execute(context.Background(), opts, append([]string{"env", "set"}, args...))
	return code, stdout.String(), stderr.String()
}

func stagingToken(t *testing.T, st *store.Store, key string) (value string, secret bool) {
	t.Helper()
	ws, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Hydrate(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	for _, env := range ws.Environments {
		if env.Name != "Staging" {
			continue
		}
		for _, v := range env.Variables {
			if v.Key == key {
				return v.Value, v.IsSecret()
			}
		}
	}
	return "", false
}

func TestEnvSetSecretReadsValueFromStdin(t *testing.T) {
	const value = "synthetic-piped-secret-0001"
	for name, input := range map[string]string{
		"lf":         value + "\n",
		"crlf":       value + "\r\n",
		"no newline": value,
		"first line": value + "\nsynthetic-ignored-second-line\n",
	} {
		t.Run(name, func(t *testing.T) {
			st := demoWorkspace(t, nil)
			code, out, errOut := envSetWith(st, input, false, nil, "staging", "fresh_token", "--secret")
			if code != ExitOK {
				t.Fatalf("code=%d stderr=%q", code, errOut)
			}
			if strings.Contains(out+errOut, value) || strings.Contains(out+errOut, "ignored") {
				t.Fatalf("the value was echoed:\n%s%s", out, errOut)
			}
			if !strings.Contains(out, "Set secret fresh_token on Staging.") {
				t.Fatalf("unexpected output %q", out)
			}
			if got, secret := stagingToken(t, st, "fresh_token"); got != value || !secret {
				t.Fatalf("stored value matches=%v secret=%v", got == value, secret)
			}
			// The YAML holds a reference, never the value.
			files, _ := filepath.Glob(filepath.Join(st.Root(), "environments", "*.yaml"))
			for _, file := range files {
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(data), value) {
					t.Fatalf("%s holds the secret in plaintext", filepath.Base(file))
				}
			}
		})
	}
}

func TestEnvSetSecretStdinErrorsSaveNothing(t *testing.T) {
	for name, input := range map[string]string{"empty stdin": "", "blank line": "\n", "crlf blank": "\r\n"} {
		t.Run(name, func(t *testing.T) {
			st := demoWorkspace(t, nil)
			code, out, errOut := envSetWith(st, input, false, nil, "staging", "fresh_token", "--secret")
			if code != ExitUsage || out != "" || !strings.Contains(errOut, "Cannot read the value") {
				t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
			}
			if _, found := stagingToken(t, st, "fresh_token"); found {
				t.Fatal("a variable was saved without a value")
			}
		})
	}
}

func TestEnvSetSecretPromptsWithoutEcho(t *testing.T) {
	const value = "synthetic-typed-secret-0002"
	st := demoWorkspace(t, nil)
	reads := 0
	code, out, errOut := envSetWith(st, "must-not-be-read\n", true,
		func() ([]byte, error) { reads++; return []byte(value), nil },
		"staging", "token", "--secret")
	if code != ExitOK || reads != 1 {
		t.Fatalf("code=%d reads=%d stderr=%q", code, reads, errOut)
	}
	if strings.Contains(out+errOut, value) || strings.Contains(out+errOut, "must-not-be-read") {
		t.Fatalf("the value was echoed:\n%s%s", out, errOut)
	}
	if !strings.Contains(errOut, "Value for token in staging (input hidden): ") {
		t.Fatalf("no prompt on stderr: %q", errOut)
	}
	if got, secret := stagingToken(t, st, "token"); got != value || !secret {
		t.Fatalf("existing variable not updated: matches=%v secret=%v", got == value, secret)
	}

	code, _, errOut = envSetWith(st, "", true,
		func() ([]byte, error) { return nil, errors.New("boom") }, "staging", "other", "--secret")
	if code != ExitUsage || !strings.Contains(errOut, "could not read from the terminal") {
		t.Fatalf("terminal error: code=%d err=%q", code, errOut)
	}
	code, _, errOut = envSetWith(st, "", true,
		func() ([]byte, error) { return []byte{}, nil }, "staging", "other", "--secret")
	if code != ExitUsage || !strings.Contains(errOut, "the value is empty") {
		t.Fatalf("empty prompt: code=%d err=%q", code, errOut)
	}
}

func TestEnvSetArgvFormStillWorksAndValueIsRequiredWithoutSecret(t *testing.T) {
	st := demoWorkspace(t, nil)
	code, _, errOut := envSetWith(st, "", false, nil, "staging", "region", "us")
	if code != ExitOK {
		t.Fatalf("argv form: code=%d err=%q", code, errOut)
	}
	if got, secret := stagingToken(t, st, "region"); got != "us" || secret {
		t.Fatalf("region = %q secret=%v", got, secret)
	}
	code, _, errOut = envSetWith(st, "piped\n", false, nil, "staging", "region2")
	if code != ExitUsage || !strings.Contains(errOut, "Missing value") || !strings.Contains(errOut, "--secret") {
		t.Fatalf("no value without --secret: code=%d err=%q", code, errOut)
	}
	code, _, errOut = envSetWith(st, "", false, nil, "staging")
	if code != ExitUsage || !strings.Contains(errOut, "[<value>]") {
		t.Fatalf("usage: code=%d err=%q", code, errOut)
	}
}
