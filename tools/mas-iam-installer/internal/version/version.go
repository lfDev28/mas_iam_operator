package version

import "fmt"

var (
	// The in-cluster installer derives its default Job image from this string
	// ("v" + Version), so the published tag must match it exactly.
	Version   = "0.1.4"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, BuildDate)
}
