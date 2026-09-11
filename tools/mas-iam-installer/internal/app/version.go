package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/version"
)

func newVersionCommand(root *RootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show the installed mas-est version",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, version.String())

			// The binary alone does not say how old the local runtime is, which
			// is the thing that actually goes stale - the wrapper execs whatever
			// bootstrap extracted, however long ago.
			if stamp := readRuntimeInfo(root.RepoRoot); stamp != "" {
				fmt.Fprintf(out, "runtime extracted: %s\n", stamp)
			}
			if requested := strings.TrimSpace(os.Getenv(masESTImageEnv)); requested != "" {
				fmt.Fprintf(out, "%s: %s\n", masESTImageEnv, requested)
				if tag, mismatch := runtimeVersionMismatch(requested, version.Version); mismatch {
					fmt.Fprintf(out, "warning: %s requests %s but this binary is v%s; re-run bootstrap to refresh\n",
						masESTImageEnv, tag, version.Version)
				}
			}
			return nil
		},
	}
}
