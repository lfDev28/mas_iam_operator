package version

import "fmt"

var (
	// Dev builds between releases. The in-cluster installer derives its default
	// Job image from this string ("v" + Version), so the published dev tag must
	// match it exactly. Docs stay pinned to the last stable release.
	Version   = "0.1.4-dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, BuildDate)
}
