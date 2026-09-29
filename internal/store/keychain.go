package store

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"

	"github.com/shubham/postctl/internal/model"
)

// DefaultKeychainService is the Keychain "service" attribute postctl uses.
const DefaultKeychainService = "com.shubham.postctl"

const securityBin = "/usr/bin/security"

// exitItemNotFound is the exit status of `security` when an item is missing.
const exitItemNotFound = 44

// Keychain is a [SecretStore] backed by the macOS login Keychain through
// /usr/bin/security (invoked without a shell). Each ref is a generic-password
// item with the ref as its account.
//
// Values are base64-encoded before storage so arbitrary bytes and newlines
// survive `security`'s text output. Secrets are passed to `security` on stdin
// (interactive mode) rather than argv, so they never appear in the process
// list.
type Keychain struct {
	// Service is the Keychain service attribute; empty means
	// [DefaultKeychainService].
	Service string

	// run is replaceable in tests.
	run func(ctx context.Context, stdin string, args ...string) (stdout string, exit int, err error)
}

// NewKeychain returns a Keychain store for service ("" for the default).
func NewKeychain(service string) *Keychain { return &Keychain{Service: service} }

func (k *Keychain) service() string {
	if k.Service == "" {
		return DefaultKeychainService
	}
	return k.Service
}

func (k *Keychain) exec(ctx context.Context, stdin string, args ...string) (string, int, error) {
	if k.run != nil {
		return k.run(ctx, stdin, args...)
	}
	if runtime.GOOS != "darwin" {
		return "", 0, errors.New("macOS Keychain is available only on macOS")
	}
	cmd := exec.CommandContext(ctx, securityBin, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard // security echoes item attributes on failure; never surface them
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.String(), ee.ExitCode(), err
	}
	return out.String(), 0, err
}

// Get implements [SecretStore].
func (k *Keychain) Get(ctx context.Context, ref model.SecretRef) (string, error) {
	out, code, err := k.exec(ctx, "", "find-generic-password", "-s", k.service(), "-a", string(ref), "-w")
	if err != nil {
		if code == exitItemNotFound {
			return "", ErrSecretNotFound
		}
		return "", errors.New("could not read a secret from the macOS Keychain")
	}
	raw, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(out))
	if derr != nil {
		return "", errors.New("a Keychain item has an unexpected format")
	}
	return string(raw), nil
}

// Set implements [SecretStore].
func (k *Keychain) Set(ctx context.Context, ref model.SecretRef, value string) error {
	if value == "" {
		return errors.New("refusing to store an empty secret")
	}
	if strings.ContainsAny(string(ref), "\x00\n\r") {
		return fmt.Errorf("invalid secret ref %q", ref)
	}
	enc := base64.StdEncoding.EncodeToString([]byte(value))
	// `security -i` reads commands from stdin, keeping the secret off argv.
	// -U updates an existing item.
	script := fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n",
		quoteSecurityArg(k.service()), quoteSecurityArg(string(ref)), enc)
	if _, _, err := k.exec(ctx, script, "-i"); err != nil {
		return errors.New("could not save a secret to the macOS Keychain")
	}
	// Interactive mode exits 0 even when a command fails, so confirm the
	// write by reading it back.
	if got, err := k.Get(ctx, ref); err != nil || got != value {
		return errors.New("could not save a secret to the macOS Keychain")
	}
	return nil
}

// Delete implements [SecretStore].
func (k *Keychain) Delete(ctx context.Context, ref model.SecretRef) error {
	_, code, err := k.exec(ctx, "", "delete-generic-password", "-s", k.service(), "-a", string(ref))
	if err != nil && code != exitItemNotFound {
		return errors.New("could not remove a secret from the macOS Keychain")
	}
	return nil
}

// quoteSecurityArg quotes s for `security -i` command parsing: double quotes,
// with backslash and quote escaped.
func quoteSecurityArg(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}
