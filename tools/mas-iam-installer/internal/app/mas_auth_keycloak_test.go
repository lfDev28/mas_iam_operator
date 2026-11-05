package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeKcadm stands in for /opt/keycloak/bin/kcadm.sh. Behaviour is driven by
// env vars; every mutating call is appended to $FAKE_LOG.
const fakeKcadm = `#!/usr/bin/env bash
set -eu
case "$1 $2" in
  "config credentials") exit 0 ;;
  "get clients")
    if [[ -n "${FAKE_GET_CLIENTS_FAIL:-}" ]]; then
      echo "HTTP 401 Unauthorized" >&2
      exit 1
    fi
    printf '%b' "${FAKE_CLIENTS:-}"
    exit 0 ;;
esac
case "$1" in
  get)
    case "$2" in
      */client-secret)
        printf '{\n  "type" : "secret",\n  "value" : "%s"\n}\n' "${FAKE_SECRET:-}"
        exit 0 ;;
      */protocol-mappers/models) exit 0 ;;
    esac ;;
  create|update)
    file=""
    while [[ $# -gt 0 ]]; do
      if [[ "$1" == "-f" ]]; then file="$2"; fi
      shift
    done
    secret_line=$(grep '"secret"' "$file" || true)
    echo "${MODE_LABEL:-call} $(grep '"clientId"' "$file" | tr -d ' ') ${secret_line// /}" >>"$FAKE_LOG"
    exit 0 ;;
esac
echo "fake kcadm: unhandled $*" >&2
exit 2
`

type kcadmRun struct {
	output string
	log    string
	err    error
}

func runClientScript(t *testing.T, env map[string]string) kcadmRun {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	kcadm := filepath.Join(dir, "kcadm.sh")
	if err := os.WriteFile(kcadm, []byte(fakeKcadm), 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	script := keycloakMASClientScript(map[string]string{
		"KC_ADMIN_USER":           "admin",
		"KC_ADMIN_PASSWORD":       "pw",
		"KC_SERVICE":              "http://kc:8080",
		"KC_REALM":                "maximo",
		"CONFIGURE_OIDC":          "true",
		"OIDC_CLIENT_ID":          "default",
		"OIDC_CLIENT_SECRET":      "freshly-generated",
		"OIDC_REDIRECT_URIS_JSON": `["https://auth.example.com/oidcclient/redirect/default"]`,
		"CONFIGURE_SAML":          "false",
	})
	cmd := exec.Command("bash", "-s")
	cmd.Stdin = strings.NewReader(script)
	cmd.Env = append(os.Environ(), "KCADM="+kcadm, "MAS_EST_KCADM_HOME="+filepath.Join(dir, "home"), "FAKE_LOG="+logPath)
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	out, err := cmd.CombinedOutput()
	logRaw, _ := os.ReadFile(logPath)
	return kcadmRun{output: string(out), log: string(logRaw), err: err}
}

func TestKeycloakClientScriptReusesExistingOIDCSecret(t *testing.T) {
	run := runClientScript(t, map[string]string{
		"FAKE_CLIENTS": `account,uuid-account\ndefault,uuid-oidc\n`,
		"FAKE_SECRET":  "kc-current-secret",
	})
	if run.err != nil {
		t.Fatalf("script failed: %v\n%s", run.err, run.output)
	}
	secret, state, ok := parseKeycloakOIDCSecret(run.output)
	if !ok || secret != "kc-current-secret" || state != "reused" {
		t.Fatalf("parsed secret=%q state=%q ok=%v, want the existing secret reused\n%s", secret, state, ok, run.output)
	}
	if strings.Contains(run.log, "freshly-generated") {
		t.Fatalf("the existing client was updated with a NEW secret (rotation):\n%s", run.log)
	}
	if !strings.Contains(run.log, `"secret":"kc-current-secret"`) {
		t.Fatalf("update did not carry the existing secret:\n%s", run.log)
	}
}

func TestKeycloakClientScriptGeneratesSecretOnCreate(t *testing.T) {
	run := runClientScript(t, map[string]string{
		"FAKE_CLIENTS": `account,uuid-account\n`,
	})
	if run.err != nil {
		t.Fatalf("script failed: %v\n%s", run.err, run.output)
	}
	secret, state, ok := parseKeycloakOIDCSecret(run.output)
	if !ok || secret != "freshly-generated" || state != "generated" {
		t.Fatalf("parsed secret=%q state=%q ok=%v, want the generated secret on create\n%s", secret, state, ok, run.output)
	}
	if !strings.Contains(run.log, `"secret":"freshly-generated"`) {
		t.Fatalf("create did not carry the generated secret:\n%s", run.log)
	}
}

func TestKeycloakClientScriptFailsWhenClientLookupFails(t *testing.T) {
	run := runClientScript(t, map[string]string{
		"FAKE_GET_CLIENTS_FAIL": "1",
	})
	if run.err == nil {
		t.Fatalf("script succeeded although 'kcadm get clients' failed:\n%s", run.output)
	}
	if strings.TrimSpace(run.log) != "" {
		t.Fatalf("script created/updated clients after a failed lookup:\n%s", run.log)
	}
	if _, _, ok := parseKeycloakOIDCSecret(run.output); ok {
		t.Fatal("a failed run must not report a secret")
	}
	if !strings.Contains(run.output, "kcadm get clients' failed") {
		t.Fatalf("missing diagnosis in output:\n%s", run.output)
	}
}

func TestParseKeycloakOIDCSecretIgnoresNoise(t *testing.T) {
	out := "Warning: Use tokens from the TokenRequest API\r\nMAS_EST_OIDC_CLIENT_SECRET_STATE=reused\r\nMAS_EST_OIDC_CLIENT_SECRET=abc123\r\n"
	secret, state, ok := parseKeycloakOIDCSecret(out)
	if !ok || secret != "abc123" || state != "reused" {
		t.Fatalf("got %q %q %v", secret, state, ok)
	}
	if _, _, ok := parseKeycloakOIDCSecret("nothing here"); ok {
		t.Fatal("ok for output without the marker")
	}
}

func TestUsableMASOIDCSecret(t *testing.T) {
	cases := []struct {
		name string
		data map[string]string
		want bool
	}{
		{"random hex", map[string]string{"clientSecret": "0f1e2d3c4b5a69788796a5b4c3d2e1f0"}, true},
		{"trimmed", map[string]string{"clientSecret": "  0f1e2d3c4b5a69788796a5b4c3d2e1f0\n"}, true},
		{"missing", map[string]string{"clientId": "default"}, false},
		{"too short", map[string]string{"clientSecret": "short"}, false},
		{"json quote", map[string]string{"clientSecret": `0f1e2d3c4b5a6978"8796a5b4c3d2e1f0`}, false},
		{"shell expansion", map[string]string{"clientSecret": "0f1e2d3c4b5a6978$(id)96a5b4c3d2e1f0"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := usableMASOIDCSecret(tc.data); ok != tc.want {
				t.Fatalf("usableMASOIDCSecret(%q) ok=%v, want %v", tc.data["clientSecret"], ok, tc.want)
			}
		})
	}
}
