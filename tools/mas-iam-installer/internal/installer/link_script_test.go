package installer

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeLinkOC simulates a cluster whose Mongo lives in namespace "mas-mongo"
// (not the mongoce default) and captures the mongosh --eval script.
const fakeLinkOC = `#!/usr/bin/env bash
args="$*"
case "$args" in
  "get secret mas-mongo-ce-admin-admin -n mongoce"*) exit 1 ;;
  "get secret -A --field-selector"*) printf '%s' "${FAKE_MONGO_NS:-}" ;;
  "get secret mas-mongo-ce-admin-admin -n mas-mongo -o"*) printf '%s' "$(printf 'mongodb://x' | base64)" ;;
  "get pod -n mas-mongo -l"*) printf 'mas-mongo-ce-0' ;;
  "exec "*)
    for a in "$@"; do last="$a"; done
    printf '%s' "$last" >"$EVAL_OUT"
    echo "[fake] exec $args" | head -c 120 ;;
  *) echo "unhandled: $args" >&2; exit 3 ;;
esac
`

// mockMongo runs the captured --eval script against a fake db and prints the
// updateOne calls as JSON.
const mockMongo = `
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const updates = [];
const users = [{ _id: 'scim.user1', emails: [{ value: 'scim.user1@demo.local', primary: true }] }];
const db = { getSiblingDB() { return { User: {
  find() { return { toArray: () => users }; },
  updateOne(filter, update) { updates.push({ filter, update }); },
} }; } };
new Function('db', 'print', src)(db, () => {});
console.log(JSON.stringify(updates));
`

func linkScriptPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(filepath.Dir(commonScriptPath(t)), "link-scim-users-oidc.sh")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("link-scim-users-oidc.sh not found: %v", err)
	}
	return path
}

func runLinkScript(t *testing.T, env map[string]string, args ...string) (string, string, error) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "oc"), []byte(fakeLinkOC), 0o755); err != nil {
		t.Fatal(err)
	}
	evalOut := filepath.Join(dir, "eval.js")
	cmd := exec.Command("bash", append([]string{linkScriptPath(t)}, args...)...)
	cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "EVAL_OUT="+evalOut,
		"MAS_EST_MONGO_NAMESPACE=", "MAS_EST_MONGO_INSTANCE=")
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	out, err := cmd.CombinedOutput()
	script, _ := os.ReadFile(evalOut)
	return string(out), string(script), err
}

func TestLinkScriptSetsOnlyTheIDPIdentity(t *testing.T) {
	out, script, err := runLinkScript(t, map[string]string{"FAKE_MONGO_NS": "mas-mongo"},
		"--instance", "inst1", "--idp", "default-oidc", "--idp-type", "oidc")
	if err != nil {
		t.Fatalf("link script failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "found in namespace mas-mongo") {
		t.Fatalf("Mongo namespace was not auto-detected:\n%s", out)
	}
	if strings.Contains(script, "identities: {") {
		t.Fatalf("script still replaces the whole identities map:\n%s", script)
	}

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available to evaluate the mongosh script")
	}
	dir := t.TempDir()
	evalPath := filepath.Join(dir, "eval.js")
	mockPath := filepath.Join(dir, "mock.js")
	if err := os.WriteFile(evalPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mockPath, []byte(mockMongo), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command(node, mockPath, evalPath).CombinedOutput()
	if err != nil {
		t.Fatalf("mongosh script does not evaluate: %v\n%s", err, raw)
	}
	var updates []struct {
		Update map[string]map[string]any `json:"update"`
	}
	if err := json.Unmarshal(raw, &updates); err != nil || len(updates) != 1 {
		t.Fatalf("updates = %s (%v)", raw, err)
	}
	set := updates[0].Update["$set"]
	unset := updates[0].Update["$unset"]
	if _, ok := set["identities.default-oidc"]; !ok {
		t.Fatalf("$set must target identities.default-oidc only: %v", set)
	}
	if _, ok := set["identities"]; ok {
		t.Fatalf("$set must not replace the identities map: %v", set)
	}
	for _, key := range []string{"identities._local", "identities.default"} {
		if _, ok := unset[key]; !ok {
			t.Fatalf("$unset must drop %s (Manage idpnotfound): %v", key, unset)
		}
	}
	if len(unset) != 2 {
		t.Fatalf("$unset must touch only the two auto-generated keys: %v", unset)
	}
}

func TestLinkScriptFailureExplainsTheFix(t *testing.T) {
	out, _, err := runLinkScript(t, nil, "--instance", "inst1", "--idp", "default-oidc")
	if err == nil {
		t.Fatalf("expected failure with no Mongo found:\n%s", out)
	}
	for _, want := range []string{"NOT linked", "MAS_EST_MONGO_NAMESPACE=", "--instance inst1 --idp default-oidc"} {
		if !strings.Contains(out, want) {
			t.Fatalf("failure output missing %q:\n%s", want, out)
		}
	}
}
