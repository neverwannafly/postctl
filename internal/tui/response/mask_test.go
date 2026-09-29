package response

import "testing"

func TestSecretHeader(t *testing.T) {
	if !SecretHeader("Authorization") || SecretHeader("Accept") {
		t.Fatal("Authorization should be secret, Accept not")
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
