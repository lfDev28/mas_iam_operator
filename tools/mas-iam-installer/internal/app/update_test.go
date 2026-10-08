package app

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.1.14", "v0.1.13", true},
		{"v0.1.13", "v0.1.13", false},
		{"v0.1.13", "v0.1.14", false},
		// Numeric, not lexical: 10 > 9.
		{"v0.1.10", "v0.1.9", true},
		{"v1.0.0", "v0.9.99", true},
		// The release supersedes its own dev build, but not the next one.
		{"v0.1.14", "v0.1.14-dev", true},
		{"v0.1.13", "v0.1.14-dev", false},
		{"garbage", "v0.1.13", false},
	}

	for _, tc := range cases {
		if got := releaseNewer(tc.a, tc.b); got != tc.want {
			t.Errorf("releaseNewer(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestLatestReleaseTagSkipsNonReleaseTags(t *testing.T) {
	pages := map[string]string{
		"1": `{"tags":[{"name":"v0.1.14-amd64"},{"name":"v0.1.14-dev"},{"name":"v0.1.9"},{"name":"v0.1.0-beta.24"}],"has_additional":true}`,
		"2": `{"tags":[{"name":"v0.1.10"},{"name":"latest"},{"name":"v0.1.2"}],"has_additional":false}`,
	}
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		fmt.Fprint(w, pages[r.URL.Query().Get("page")])
	}))
	defer server.Close()

	got, err := latestReleaseTag(context.Background(), server.Client(), server.URL, "quay.io/lee_forster/mas-external-services-tool")
	if err != nil {
		t.Fatal(err)
	}
	if got != "v0.1.10" {
		t.Fatalf("latest = %q, want v0.1.10", got)
	}
	if len(paths) != 2 || paths[0] != "/repository/lee_forster/mas-external-services-tool/tag/" {
		t.Fatalf("unexpected requests: %v", paths)
	}
}

func TestLatestReleaseTagErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tags":[{"name":"v0.1.14-dev"}],"has_additional":false}`)
	}))
	defer server.Close()

	if _, err := latestReleaseTag(context.Background(), server.Client(), server.URL, "quay.io/a/b"); err == nil {
		t.Fatal("want error when no release tag exists")
	}
	if _, err := latestReleaseTag(context.Background(), server.Client(), server.URL, "registry.example.com/a/b"); err == nil {
		t.Fatal("want error for a non-quay repository")
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer failing.Close()
	if _, err := latestReleaseTag(context.Background(), failing.Client(), failing.URL, "quay.io/a/b"); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("want HTTP 404 error, got %v", err)
	}
}

func TestLauncherLocation(t *testing.T) {
	dir := t.TempDir()
	repoRoot := filepath.Join(dir, ".mas-est-runtime", "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mas-est"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	installDir, command, err := launcherLocation(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if installDir != dir || command != "mas-est" {
		t.Fatalf("got (%q, %q), want (%q, mas-est)", installDir, command, dir)
	}

	// The installer image and a source checkout are not bootstrapped runtimes.
	for _, bad := range []string{"", "/opt/mas-est", filepath.Join(dir, "checkout", "repo"), filepath.Join(dir, ".other-runtime", "repo")} {
		if _, _, err := launcherLocation(bad); err == nil {
			t.Errorf("launcherLocation(%q) = nil error, want refusal", bad)
		}
	}
}

func TestUpdateAlreadyCurrent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tags":[{"name":"v0.0.1"}],"has_additional":false}`)
	}))
	defer server.Close()

	var out bytes.Buffer
	opts := &updateOptions{
		root:       &RootOptions{},
		repository: "quay.io/lee_forster/mas-external-services-tool",
		apiBase:    server.URL,
		httpClient: server.Client(),
	}
	// An older release on Quay must never trigger a downgrade, and must not
	// reach the launcher lookup (RepoRoot is empty here, which would fail).
	if err := opts.run(context.Background(), &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "already up to date") {
		t.Fatalf("output = %q", out.String())
	}
}
