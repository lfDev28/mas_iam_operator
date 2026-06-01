package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestRuntimeConfigDisplayValueRedactsSecrets(t *testing.T) {
	cases := []struct {
		name   string
		key    string
		value  string
		secret bool
		want   string
	}{
		{
			name:   "normal configmap value",
			key:    "SCIM_BRIDGE_MAS_BASE_URL",
			value:  "https://api.example/scim/v2",
			secret: false,
			want:   "https://api.example/scim/v2",
		},
		{
			name:   "secret key always redacted",
			key:    "SCIM_BRIDGE_KEYCLOAK_CLIENT_ID",
			value:  "scim-admin",
			secret: true,
			want:   redactedRuntimeValue,
		},
		{
			name:   "sensitive configmap key redacted",
			key:    "LDAP_BIND_PASSWORD",
			value:  "ldap-password",
			secret: false,
			want:   redactedRuntimeValue,
		},
		{
			name:   "mas api token key redacted",
			key:    "SCIM_BRIDGE_MAS_API_TOKEN_VALUE",
			value:  "mas-token-value",
			secret: false,
			want:   redactedRuntimeValue,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := runtimeConfigDisplayValue(tt.key, tt.value, tt.secret); got != tt.want {
				t.Fatalf("runtimeConfigDisplayValue() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrintRuntimeSecretKeysOnlyPrintsKeyNames(t *testing.T) {
	var out bytes.Buffer
	printRuntimeSecretKeys(&out, []string{
		"SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET",
		"SCIM_BRIDGE_MAS_API_TOKEN_VALUE",
	})

	output := out.String()
	for _, want := range []string{
		"SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET=[REDACTED]",
		"SCIM_BRIDGE_MAS_API_TOKEN_VALUE=[REDACTED]",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	for _, leaked := range []string{"keycloak-secret", "mas-token-value"} {
		if strings.Contains(output, leaked) {
			t.Fatalf("output leaked %q:\n%s", leaked, output)
		}
	}
}

func TestMASAPITokenSecretMergePatchPreservesOtherKeysByOnlyTargetingTokenKeys(t *testing.T) {
	patch, err := masAPITokenSecretMergePatch("new-name", "new-value")
	if err != nil {
		t.Fatal(err)
	}

	var payload struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(patch, &payload); err != nil {
		t.Fatal(err)
	}

	if len(payload.Data) != 2 {
		t.Fatalf("patch should only include token keys, got: %#v", payload.Data)
	}
	if got := decodeTestSecretValue(t, payload.Data[masAPITokenNameKey]); got != "new-name" {
		t.Fatalf("token name = %q, want new-name", got)
	}
	if got := decodeTestSecretValue(t, payload.Data[masAPITokenValueKey]); got != "new-value" {
		t.Fatalf("token value = %q, want new-value", got)
	}
}

func TestConfigMapMergePatchOnlyTargetsProvidedBridgeKeys(t *testing.T) {
	patch, err := configMapMergePatch(map[string]string{
		bridgeLogLevelKey:   "debug",
		bridgePayloadLogKey: "true",
	})
	if err != nil {
		t.Fatal(err)
	}

	var payload struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(patch, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 2 {
		t.Fatalf("patch should only include bridge keys, got: %#v", payload.Data)
	}
	if payload.Data[bridgeLogLevelKey] != "debug" {
		t.Fatalf("log level = %q, want debug", payload.Data[bridgeLogLevelKey])
	}
	if payload.Data[bridgePayloadLogKey] != "true" {
		t.Fatalf("payload logging = %q, want true", payload.Data[bridgePayloadLogKey])
	}
}

func TestSortedMapKeys(t *testing.T) {
	keys := sortedMapKeys(map[string]string{"b": "2", "a": "1"})
	if strings.Join(keys, ",") != "a,b" {
		t.Fatalf("sortedMapKeys() = %#v", keys)
	}
}

func decodeTestSecretValue(t *testing.T, encoded string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
