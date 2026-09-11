package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/version"
)

// mas-est is normally run through a wrapper script that execs a binary
// extracted into ~/mas-est/.mas-est-runtime at bootstrap time. Nothing
// refreshes that binary except re-running bootstrap, and until now nothing
// reported which version it was: `mas-est install` silently ran whatever was
// extracted months ago.
//
// That matters more here than in most CLIs, because the in-cluster installer's
// RBAC is applied by THIS binary using the user's credentials - not by the Job
// image. A stale launcher reapplies stale permissions, so a fix published
// minutes ago appears not to work and the only way to tell is to read the live
// Role off the cluster. It cost two failed installs on 2026-08-31.
//
// Exporting MAS_EST_IMAGE does not refresh the runtime; it only selects which
// image `podman run` bootstraps from. So when the env var names a tag that
// disagrees with this binary's own version, the user believes they are running
// a version they are not. That is the check below. It is deliberately local -
// no registry lookup - so it cannot fail in airgapped or proxied environments.

const (
	masESTImageEnv        = "MAS_EST_IMAGE"
	runtimeInfoFileName   = "runtime-info"
	runtimeInfoVersionKey = "version"
	runtimeInfoStampKey   = "extracted"
)

// printVersionBanner announces the running version. It goes to the tee'd
// output so it lands in the execution log: an install log that does not state
// its version is nearly useless for support triage.
func printVersionBanner(out io.Writer) {
	fmt.Fprintf(out, "[version] mas-est v%s\n", version.Version)
}

// imageRefTag extracts the tag from an image reference. Returns "" when the
// reference carries no tag, or is pinned by digest (nothing to compare).
func imageRefTag(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.Contains(ref, "@") {
		return ""
	}

	colon := strings.LastIndex(ref, ":")
	if colon < 0 {
		return ""
	}
	// A colon before the final slash is a registry port (host:5000/img), not a
	// tag separator.
	if strings.Contains(ref[colon+1:], "/") {
		return ""
	}

	return strings.TrimSpace(ref[colon+1:])
}

// runtimeVersionMismatch reports whether MAS_EST_IMAGE names a different
// version than this binary. Returns the offending tag when they disagree.
func runtimeVersionMismatch(imageRef, binaryVersion string) (string, bool) {
	tag := imageRefTag(imageRef)
	if tag == "" {
		return "", false
	}

	want := "v" + strings.TrimSpace(binaryVersion)
	if strings.EqualFold(tag, want) {
		return "", false
	}

	return tag, true
}

// checkRuntimeVersion fails the run when the environment disagrees with the
// binary. Hard failure rather than a warning is deliberate: an install with the
// wrong launcher is expensive, silently wrong, and hard to diagnose after the
// fact.
func checkRuntimeVersion(skip bool) error {
	if skip {
		return nil
	}

	imageRef := strings.TrimSpace(os.Getenv(masESTImageEnv))
	tag, mismatch := runtimeVersionMismatch(imageRef, version.Version)
	if !mismatch {
		return nil
	}

	return fmt.Errorf(`stale local runtime detected

  %s requests : %s
  this binary is : v%s

The mas-est command execs a binary extracted at bootstrap time; exporting
%s does not refresh it. Re-run bootstrap to pick up %s:

  podman run -ti --rm -v "$HOME/mas-est:/tmp" --pull always "$%s"

Then confirm with: mas-est version
Pass --skip-version-check to run this binary anyway`,
		masESTImageEnv, tag, version.Version, masESTImageEnv, tag, masESTImageEnv)
}

// writeRuntimeInfo records what bootstrap extracted and when, so a support
// bundle or `mas-est version` can show the age of a runtime rather than only
// its version string.
func writeRuntimeInfo(runtimeDir string) error {
	contents := fmt.Sprintf("%s=%s\n%s=%s\n",
		runtimeInfoVersionKey, version.Version,
		runtimeInfoStampKey, time.Now().UTC().Format(time.RFC3339))

	return os.WriteFile(filepath.Join(runtimeDir, runtimeInfoFileName), []byte(contents), 0o644)
}

// readRuntimeInfo returns the recorded extraction stamp for the runtime this
// binary was launched from, or "" when it cannot be determined (a binary run
// directly from a build, or a runtime predating this file).
func readRuntimeInfo(repoRoot string) string {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return ""
	}

	raw, err := os.ReadFile(filepath.Join(filepath.Dir(repoRoot), runtimeInfoFileName))
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(string(raw), "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if found && key == runtimeInfoStampKey {
			return strings.TrimSpace(value)
		}
	}

	return ""
}
