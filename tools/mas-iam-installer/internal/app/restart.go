package app

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	executil "github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/exec"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/ui"
)

type restartBridgeOptions struct {
	namespace string
}

func newRestartCommand() *cobra.Command {
	defaults := config.LoadInstallConfigFromEnv()
	command := &cobra.Command{
		Use:   "restart",
		Short: "Restart MAS EST runtime components",
	}

	bridgeOpts := &restartBridgeOptions{
		namespace: defaults.Namespace,
	}
	bridgeCommand := &cobra.Command{
		Use:   "bridge",
		Short: "Restart the SCIM bridge deployment and wait for rollout",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return bridgeOpts.run(cmd.Context())
		},
	}
	bridgeCommand.Flags().StringVar(&bridgeOpts.namespace, "namespace", bridgeOpts.namespace, "Target namespace")

	command.AddCommand(bridgeCommand)
	return command
}

func (o *restartBridgeOptions) run(ctx context.Context) error {
	runner := executil.NewRunner()
	client := oc.NewClient(runner)
	if _, err := ensureClusterLogin(ctx, runner, client, ui.IsInteractive()); err != nil {
		return err
	}
	return restartBridge(ctx, client, defaultedNamespace(o.namespace))
}
