package mas

import (
	"strings"
	"testing"
)

func TestRedactPayloadRedactsSensitiveNestedFields(t *testing.T) {
	output := RedactPayload([]byte(`{
		"userName":"alice",
		"password":"secret-password",
		"profile":{"apiToken":"secret-token","displayName":"Alice"}
	}`))

	for _, leaked := range []string{"secret-password", "secret-token"} {
		if payloadContains([]byte(output), leaked) {
			t.Fatalf("redacted payload leaked %q: %s", leaked, output)
		}
	}
	for _, want := range []string{"alice", "Alice", redactedValue} {
		if !payloadContains([]byte(output), want) {
			t.Fatalf("redacted payload missing %q: %s", want, output)
		}
	}
}

func TestRedactPayloadReturnsRedactedForInvalidJSON(t *testing.T) {
	if got := RedactPayload([]byte(`not-json`)); got != redactedValue {
		t.Fatalf("RedactPayload() = %q, want %q", got, redactedValue)
	}
}

func payloadContains(raw []byte, needle string) bool {
	return strings.Contains(string(raw), needle)
}
