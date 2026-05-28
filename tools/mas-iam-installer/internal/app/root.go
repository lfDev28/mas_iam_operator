package app

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
)

type RootOptions struct {
	RepoRoot string
}

func NewRootCommand() *cobra.Command {
	rootOptions := &RootOptions{
		RepoRoot: os.Getenv(config.EnvRepoRoot),
	}

	command := &cobra.Command{
		Use:           "mas-iam",
		Short:         "Install and manage MAS IAM on OpenShift",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	command.CompletionOptions.DisableDefaultCmd = true

	command.PersistentFlags().StringVar(&rootOptions.RepoRoot, "repo-root", rootOptions.RepoRoot, "Override repo root containing scripts and manifests")
	_ = command.PersistentFlags().MarkHidden("repo-root")

	bootstrapCommand := newBootstrapCommand()
	bootstrapCommand.Hidden = true

	command.AddCommand(
		bootstrapCommand,
		newInstallCommand(rootOptions),
		newWipeCommand(rootOptions),
		newPreflightCommand(),
		newStatusCommand(),
		newSupportBundleCommand(),
		newConfigCommand(),
		newObjectStorageCommand(),
		newLogsCommand(),
		newLDAPInfoCommand(),
		newVersionCommand(),
		newRenderTemplateCommand(),
	)

	return command
}
