package app

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	executil "github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/exec"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/installer"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/logging"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/ui"
)

type wipeOptions struct {
	config         config.WipeConfig
	nonInteractive bool
	yes            bool
}

func newWipeCommand(root *RootOptions) *cobra.Command {
	opts := &wipeOptions{
		config: config.LoadWipeConfigFromEnv(),
	}

	command := &cobra.Command{
		Use:   "wipe",
		Short: "Remove the MAS EST namespace and optional MAS profile data",
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.run(cmd.Context(), root)
		},
	}

	flags := command.Flags()
	flags.StringVar(&opts.config.Namespace, "namespace", opts.config.Namespace, "Target namespace")
	flags.StringVar(&opts.config.ProfileID, "profile-id", opts.config.ProfileID, "MAS profile ID")
	flags.BoolVar(&opts.config.SkipProfileDelete, "skip-profile-delete", opts.config.SkipProfileDelete, "Skip MAS profile deletion")
	flags.StringVar(&opts.config.MASBaseURL, "mas-base-url", opts.config.MASBaseURL, "MAS SCIM base URL for profile deletion")
	flags.StringVar(&opts.config.MASAPITokenName, "mas-api-token-name", opts.config.MASAPITokenName, "MAS API token name for profile deletion")
	flags.StringVar(&opts.config.MASAPITokenValue, "mas-api-token-value", opts.config.MASAPITokenValue, "MAS API token value for profile deletion")
	flags.BoolVar(&opts.nonInteractive, "non-interactive", false, "Disable prompts and require flags/env vars")
	flags.BoolVarP(&opts.yes, "yes", "y", false, "Confirm destructive wipe without prompting")

	return command
}

func (o *wipeOptions) run(ctx context.Context, root *RootOptions) error {
	cfg := o.config
	interactive := ui.IsInteractive() && !o.nonInteractive

	if interactive {
		runner := executil.NewRunner()
		client := oc.NewClient(runner)
		cluster, err := ensureClusterLogin(ctx, runner, client, interactive)
		if err != nil {
			return err
		}
		ui.PrintClusterContext("Wipe", cluster.User, cluster.Server, cfg.Namespace, "")

		updated, err := ui.PromptWipe(cfg)
		if err != nil {
			return err
		}
		cfg = updated
		confirmed, err := ui.Confirm(fmt.Sprintf("Delete namespace %s?", cfg.Namespace), false)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(os.Stdout, "[result] wipe cancelled")
			return nil
		}
	} else {
		if err := cfg.Validate(); err != nil {
			return err
		}
		if !o.yes {
			return fmt.Errorf("--yes is required in non-interactive mode for wipe")
		}
	}

	paths, err := installer.DiscoverPaths(root.RepoRoot)
	if err != nil {
		return err
	}

	output := io.Writer(os.Stdout)
	logPath := ""
	if logFile, path, err := logging.OpenRunLog(paths.RepoRoot, "wipe"); err == nil {
		defer logFile.Close()
		output = io.MultiWriter(os.Stdout, logFile)
		logPath = path
		fmt.Fprintf(os.Stdout, "[logs] writing execution log to %s\n", path)
	} else {
		fmt.Fprintf(os.Stderr, "[warn] unable to create log file: %v\n", err)
	}

	if err := installer.RunWipe(ctx, executil.NewRunner(), paths, cfg, output); err != nil {
		if logPath != "" {
			fmt.Fprintf(os.Stderr, "[result] wipe failed; see %s\n", logPath)
		}
		return err
	}

	fmt.Fprintln(output, "[result] wipe completed")
	return nil
}
