package images

import (
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

const sample = `# header
images:
  # --- section comment
  - ref: quay.io/a/cli:v1
    kind: release
    components: [installer]
  - ref: quay.io/a/op:0.0.1   # trailing comment
    kind: release
    archs: [amd64]
    components: [operator]
  - ref: quay.io/k/keycloak:26
    kind: runtime
    components: [keycloak, scim]
  - ref: ghcr.io/m/mailpit:v1
    kind: runtime
    components: [smtp]
  - ref: docker.io/b/exporter:1
    kind: runtime
    optional: true
    components: [keycloak]
  - ref: docker.io/golang:1
    kind: build

allowlist:
  - ref: quay.io/example/never:1
    reason: placeholder
`

func TestParseSkipsAllowlistAndReadsFields(t *testing.T) {
	list, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 6 {
		t.Fatalf("got %d entries, want 6: %+v", len(list), list)
	}
	op := list[1]
	if op.Ref != "quay.io/a/op:0.0.1" || op.Kind != "release" || !reflect.DeepEqual(op.Archs, []string{"amd64"}) || !reflect.DeepEqual(op.Components, []string{"operator"}) {
		t.Fatalf("operator entry parsed as %+v", op)
	}
	if !list[4].Optional {
		t.Fatalf("optional not parsed: %+v", list[4])
	}
}

func refs(list []Image) []string {
	var out []string
	for _, img := range list {
		out = append(out, img.Ref)
	}
	return out
}

func TestForComponents(t *testing.T) {
	list, _ := Parse(strings.NewReader(sample))
	cases := []struct {
		name       string
		components []string
		inCluster  bool
		optional   bool
		want       []string
	}{
		{"smtp only, local", []string{"smtp"}, false, false, []string{"ghcr.io/m/mailpit:v1"}},
		{"keycloak pulls the operator", []string{"ldap", "keycloak"}, false, false, []string{"quay.io/a/op:0.0.1", "quay.io/k/keycloak:26"}},
		{"in-cluster adds the installer", []string{"smtp"}, true, false, []string{"quay.io/a/cli:v1", "ghcr.io/m/mailpit:v1"}},
		{"optional on request", []string{"keycloak"}, false, true, []string{"quay.io/a/op:0.0.1", "quay.io/k/keycloak:26", "docker.io/b/exporter:1"}},
	}
	for _, tc := range cases {
		if got := refs(ForComponents(list, tc.components, tc.inCluster, tc.optional)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMirrorRef(t *testing.T) {
	cases := map[string]string{
		"quay.io/lee_forster/mas-iam-operator:catalog-0.0.17": "reg.example.com/mas-est/lee_forster/mas-iam-operator:catalog-0.0.17",
		"ghcr.io/axllent/mailpit:v1.30.1":                     "reg.example.com/mas-est/axllent/mailpit:v1.30.1",
		"docker.io/bitnami/postgresql@sha256:abc":             "reg.example.com/mas-est/bitnami/postgresql@sha256:abc",
		"localhost:5000/x/y:1":                                "reg.example.com/mas-est/x/y:1",
		"golang:1.25":                                         "reg.example.com/mas-est/library/golang:1.25",
	}
	for in, want := range cases {
		if got := MirrorRef(in, "reg.example.com/mas-est/"); got != want {
			t.Errorf("MirrorRef(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Repo("quay.io/keycloak/keycloak:26.0.5"); got != "quay.io/keycloak/keycloak" {
		t.Errorf("Repo = %q", got)
	}
	if got := Repo("registry.example.com:5000/a/b"); got != "registry.example.com:5000/a/b" {
		t.Errorf("Repo with port and no tag = %q", got)
	}
}

// The real images.yaml: every image pulled on the cluster must say which
// components pull it, or `mas-est images` silently leaves it out of a mirror.
func TestRepoImagesYAMLComponents(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
	list, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	valid := map[string]bool{ComponentInstaller: true, ComponentOperator: true, "ldap": true, "keycloak": true, "scim": true, "s3": true, "smtp": true}
	installers := 0
	for _, img := range list {
		pulledOnCluster := img.Kind == "runtime" || img.Kind == "release"
		if pulledOnCluster && len(img.Components) == 0 {
			t.Errorf("%s (kind %s) has no components:", img.Ref, img.Kind)
		}
		if !pulledOnCluster && len(img.Components) > 0 {
			t.Errorf("%s (kind %s) is not pulled on the cluster but lists components", img.Ref, img.Kind)
		}
		for _, c := range img.Components {
			if !valid[c] {
				t.Errorf("%s: unknown component %q", img.Ref, c)
			}
			if c == ComponentInstaller {
				installers++
			}
		}
	}
	if installers != 1 {
		t.Errorf("want exactly one installer image, got %d", installers)
	}
	all := ForComponents(list, []string{"ldap", "keycloak", "scim", "s3", "smtp"}, true, false)
	if len(all) < 10 {
		t.Errorf("a full install lists only %d images: %v", len(all), refs(all))
	}
}
