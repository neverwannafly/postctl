package response

import "testing"

func TestDetectBodyKind(t *testing.T) {
	tests := []struct {
		ct   string
		body string
		want BodyKind
	}{
		{"application/json", `{}`, BodyJSON},
		{"text/html", `<html></html>`, BodyHTML},
		{"", `{"a":1}`, BodyJSON},
		{"application/octet-stream", "\x00\x01", BodyBinary},
		{"text/plain", "hello", BodyText},
	}
	for _, tc := range tests {
		if got := DetectBodyKind(tc.ct, []byte(tc.body)); got != tc.want {
			t.Fatalf("DetectBodyKind(%q) = %v, want %v", tc.ct, got, tc.want)
		}
	}
}
