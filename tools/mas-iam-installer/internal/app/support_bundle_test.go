package app

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestRedactedSecretSummaryOmitsValuesAndSortsKeys(t *testing.T) {
	raw := `{
		"items": [
			{
				"metadata": {"name": "scim-bridge-secret"},
				"type": "Opaque",
				"data": {
					"SCIM_BRIDGE_MAS_API_TOKEN_VALUE": "` + base64.StdEncoding.EncodeToString([]byte("mas-token-value")) + `",
					"SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET": "` + base64.StdEncoding.EncodeToString([]byte("keycloak-secret")) + `"
				}
			}
		]
	}`

	summary := redactedSecretSummaryFromJSON([]byte(raw))

	for _, want := range []string{
		"secret/scim-bridge-secret",
		"type: Opaque",
		"- SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET",
		"- SCIM_BRIDGE_MAS_API_TOKEN_VALUE",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
	for _, leaked := range []string{"mas-token-value", "keycloak-secret"} {
		if strings.Contains(summary, leaked) {
			t.Fatalf("summary leaked secret value %q:\n%s", leaked, summary)
		}
	}
}

func TestSupportRedactorRedactsSecretValuesAndCommonPatterns(t *testing.T) {
	redactor := newSupportRedactor([]string{"mas-token-value", "ldap-password", "keycloak-client-secret"})

	input := strings.Join([]string{
		"Authorization: Bearer abc.def.ghi",
		"token=mas-token-value",
		"ldap password: ldap-password",
		"client_secret=keycloak-client-secret",
	}, "\n")

	output := redactor.Redact(input)
	for _, leaked := range []string{"abc.def.ghi", "mas-token-value", "ldap-password", "keycloak-client-secret"} {
		if strings.Contains(output, leaked) {
			t.Fatalf("redacted output leaked %q:\n%s", leaked, output)
		}
	}
	if got := strings.Count(output, "[REDACTED]"); got < 4 {
		t.Fatalf("expected at least 4 redactions, got %d:\n%s", got, output)
	}
}

func TestRedactorFromSecretListJSONOnlyUsesSensitiveKeys(t *testing.T) {
	raw := `{
		"items": [
			{
				"data": {
					"SCIM_BRIDGE_MAS_API_TOKEN_VALUE": "` + base64.StdEncoding.EncodeToString([]byte("mas-token-value")) + `",
					"hostname": "` + base64.StdEncoding.EncodeToString([]byte("example.internal")) + `"
				}
			}
		]
	}`

	redactor, err := redactorFromSecretListJSON([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}

	output := redactor.Redact("token=mas-token-value host=example.internal")
	if strings.Contains(output, "mas-token-value") {
		t.Fatalf("sensitive value was not redacted:\n%s", output)
	}
	if !strings.Contains(output, "example.internal") {
		t.Fatalf("non-sensitive hostname should not be redacted:\n%s", output)
	}
}

func TestSafePathTokenAndBundleDirName(t *testing.T) {
	when := time.Date(2026, 5, 7, 8, 9, 10, 0, time.UTC)
	parent := t.TempDir()
	dir, err := createSupportBundleDir(parent, "mas-est/test namespace", when)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasSuffix(dir, "mas-est-support-mas-est-test-namespace-20260507-080910") {
		t.Fatalf("unexpected bundle dir: %s", dir)
	}

	secondDir, err := createSupportBundleDir(parent, "mas-est/test namespace", when)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(secondDir, "mas-est-support-mas-est-test-namespace-20260507-080910-01") {
		t.Fatalf("unexpected collision-safe bundle dir: %s", secondDir)
	}
}
