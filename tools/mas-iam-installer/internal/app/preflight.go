package app

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	executil "github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/exec"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/preflight"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/ui"
)

type preflightOptions struct {
	namespace      string
	masBaseURL     string
	nonInteractive bool
}

func newPreflightCommand() *cobra.Command {
	defaults := config.LoadInstallConfigFromEnv()
	opts := &preflightOptions{
		namespace:  defaults.Namespace,
		masBaseURL: defaults.MASBaseURL,
	}

	command := &cobra.Command{
		Use:   "preflight",
		Short: "Check cluster access, storage classes, and MAS endpoint settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.run(cmd.Context())
		},
	}

	flags := command.Flags()
	flags.StringVar(&opts.namespace, "namespace", opts.namespace, "Target namespace")
	flags.StringVar(&opts.masBaseURL, "mas-base-url", opts.masBaseURL, "MAS SCIM base URL, including /scim/v2")
	flags.BoolVar(&opts.nonInteractive, "non-interactive", false, "Disable prompts and require flags/env vars")

	return command
}

func (o *preflightOptions) run(ctx context.Context) error {
	interactive := ui.IsInteractive() && !o.nonInteractive
	client := oc.NewClient(executil.NewRunner())

	if interactive && (o.namespace == "" || o.masBaseURL == "") {
		if err := client.CheckAvailable(); err != nil {
			return err
		}
		user, server, err := client.WhoAmI(ctx)
		if err != nil {
			return err
		}
		ui.PrintClusterContext("Preflight", user, server, o.namespace, "")
		namespace, masBaseURL, err := ui.PromptPreflight(o.namespace, o.masBaseURL)
		if err != nil {
			return err
		}
		o.namespace = namespace
		o.masBaseURL = masBaseURL
	}

	report := preflight.Run(ctx, client, preflight.Input{
		Namespace:  o.namespace,
		MASBaseURL: o.masBaseURL,
	})
	preflight.Print(os.Stdout, report)

	if report.HasFailures() {
		return fmt.Errorf("preflight failed")
	}
	return nil
}
