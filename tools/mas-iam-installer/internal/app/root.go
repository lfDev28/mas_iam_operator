package app

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
)

type RootOptions struct {
	RepoRoot string
}

func NewRootCommand() *cobra.Command {
	rootOptions := &RootOptions{
		RepoRoot: firstEnv(config.EnvRepoRoot, config.LegacyEnvRepoRoot),
	}

	command := &cobra.Command{
		Use:           "mas-est",
		Short:         "Install and manage MAS external services on OpenShift",
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
		newUninstallCommand(rootOptions),
		newPreflightCommand(),
		newStatusCommand(),
		newSupportBundleCommand(),
		newConfigCommand(),
		newRestartCommand(),
		newMASAuthCommand(),
		newObjectStorageCommand(),
		newSMTPCommand(),
		newDetailsCommand(),
		newLogsCommand(),
		newLDAPInfoCommand(),
		newVersionCommand(),
		newRenderTemplateCommand(),
	)

	return command
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}
