package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageRefTag(t *testing.T) {
	cases := []struct {
		name string
		ref  string
		want string
	}{
		{"plain tag", "quay.io/lee_forster/mas-external-services-tool:v0.1.7", "v0.1.7"},
		{"dev tag", "quay.io/lee_forster/mas-external-services-tool:v0.1.8-dev", "v0.1.8-dev"},
		{"no tag", "quay.io/lee_forster/mas-external-services-tool", ""},
		// A colon before the final slash is a registry port, not a tag.
		{"registry port, no tag", "localhost:5000/mas-external-services-tool", ""},
		{"registry port with tag", "localhost:5000/mas-external-services-tool:v0.1.7", "v0.1.7"},
		// Digest pins carry no version to compare against.
		{"digest pin", "quay.io/lee_forster/tool@sha256:abc123", ""},
		{"empty", "", ""},
		{"whitespace", "   ", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := imageRefTag(tc.ref); got != tc.want {
				t.Fatalf("imageRefTag(%q) = %q, want %q", tc.ref, got, tc.want)
			}
		})
	}
}

func TestRuntimeVersionMismatch(t *testing.T) {
	const repo = "quay.io/lee_forster/mas-external-services-tool"

	cases := []struct {
		name         string
		ref          string
		binary       string
		wantTag      string
		wantMismatch bool
	}{
		// The exact failure from 2026-08-31: env asks for the release that
		// carries the fix, the extracted binary is the one that does not.
		{"stale runtime", repo + ":v0.1.7", "0.1.5", "v0.1.7", true},
		{"matching", repo + ":v0.1.7", "0.1.7", "", false},
		{"matching dev tag", repo + ":v0.1.8-dev", "0.1.8-dev", "", false},
		// Nothing to compare: never block the run on these.
		{"unset env", "", "0.1.7", "", false},
		{"digest pin", repo + "@sha256:abc", "0.1.7", "", false},
		{"untagged ref", repo, "0.1.7", "", false},
		{"newer binary than env", repo + ":v0.1.5", "0.1.7", "v0.1.5", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tag, mismatch := runtimeVersionMismatch(tc.ref, tc.binary)
			if mismatch != tc.wantMismatch || tag != tc.wantTag {
				t.Fatalf("runtimeVersionMismatch(%q, %q) = (%q, %v), want (%q, %v)",
					tc.ref, tc.binary, tag, mismatch, tc.wantTag, tc.wantMismatch)
			}
		})
	}
}

func TestCheckRuntimeVersionSkipBypassesMismatch(t *testing.T) {
	t.Setenv(masESTImageEnv, "quay.io/lee_forster/mas-external-services-tool:v9.9.9")

	if err := checkRuntimeVersion(true); err != nil {
		t.Fatalf("--skip-version-check must bypass the guard, got %v", err)
	}

	err := checkRuntimeVersion(false)
	if err == nil {
		t.Fatal("expected a mismatch error without --skip-version-check")
	}
	// The message has to name the fix, not just the problem - this fires on a
	// user who already believes they are on the right version.
	// "bootstrap --force" specifically: this error only fires when a runtime
	// already exists, and bare bootstrap refuses to overwrite one - so a remedy
	// without --force fails for exactly the user it is addressed to.
	for _, want := range []string{"v9.9.9", "bootstrap --force", "--skip-version-check"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error message missing %q: %v", want, err)
		}
	}
}

func TestCheckRuntimeVersionAllowsUnsetEnv(t *testing.T) {
	t.Setenv(masESTImageEnv, "")

	if err := checkRuntimeVersion(false); err != nil {
		t.Fatalf("unset %s must not block the run, got %v", masESTImageEnv, err)
	}
}

func TestRuntimeInfoRoundTrip(t *testing.T) {
	runtimeDir := t.TempDir()
	if err := writeRuntimeInfo(runtimeDir); err != nil {
		t.Fatalf("writeRuntimeInfo: %v", err)
	}

	// readRuntimeInfo is given the repo root, and derives the runtime dir from
	// it - matching how the launcher exports MAS_EST_REPO_ROOT.
	repoRoot := filepath.Join(runtimeDir, "repo")
	stamp := readRuntimeInfo(repoRoot)
	if stamp == "" {
		t.Fatal("expected a recorded extraction stamp")
	}
	if !strings.Contains(stamp, "T") || !strings.HasSuffix(stamp, "Z") {
		t.Fatalf("stamp %q is not RFC3339 UTC", stamp)
	}
}

func TestReadRuntimeInfoMissingIsQuiet(t *testing.T) {
	// A runtime extracted by an older bootstrap has no runtime-info file; that
	// must degrade to "unknown", never error.
	if got := readRuntimeInfo(filepath.Join(t.TempDir(), "repo")); got != "" {
		t.Fatalf("expected empty stamp for a missing file, got %q", got)
	}
	if got := readRuntimeInfo(""); got != "" {
		t.Fatalf("expected empty stamp for an empty repo root, got %q", got)
	}
}

func TestWriteRuntimeInfoContents(t *testing.T) {
	runtimeDir := t.TempDir()
	if err := writeRuntimeInfo(runtimeDir); err != nil {
		t.Fatalf("writeRuntimeInfo: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(runtimeDir, runtimeInfoFileName))
	if err != nil {
		t.Fatalf("read runtime info: %v", err)
	}
	if !strings.Contains(string(raw), runtimeInfoVersionKey+"=") {
		t.Fatalf("runtime info missing version key: %q", raw)
	}
}
