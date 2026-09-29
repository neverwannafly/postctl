package response

import "testing"

func TestSecretHeader(t *testing.T) {
	if !SecretHeader("Authorization") || !SecretHeader("Set-Cookie") || !SecretHeader("X-Custom-Token") || SecretHeader("Accept") {
		t.Fatal("credential-bearing headers should be secret, Accept should not")
	}
}

func TestSessionCookie(t *testing.T) {
	if !SessionCookie(nameFlags{Name: "session_id", Session: false}) {
		t.Fatal("session_id should mask")
	}
	if SessionCookie(nameFlags{Name: "theme", Session: false}) {
		t.Fatal("theme should not mask")
	}
}
