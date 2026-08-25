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
	namespace       string
	masBaseURL      string
	masAPITokenName string
	masAPITokenVal  string
	nonInteractive  bool
}

func newPreflightCommand() *cobra.Command {
	defaults := config.LoadInstallConfigFromEnv()
	opts := &preflightOptions{
		namespace:       defaults.Namespace,
		masBaseURL:      defaults.MASBaseURL,
		masAPITokenName: defaults.MASAPITokenName,
		masAPITokenVal:  defaults.MASAPITokenValue,
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
	flags.StringVar(&opts.masAPITokenName, "mas-api-token-name", opts.masAPITokenName, "MAS API token name")
	flags.StringVar(&opts.masAPITokenVal, "mas-api-token-value", opts.masAPITokenVal, "MAS API token value")
	flags.BoolVar(&opts.nonInteractive, "non-interactive", false, "Disable prompts and require flags/env vars")

	return command
}

func (o *preflightOptions) run(ctx context.Context) error {
	interactive := ui.IsInteractive() && !o.nonInteractive
	runner := executil.NewRunner()
	client := oc.NewClient(runner)

	if interactive && (o.namespace == "" || o.masBaseURL == "") {
		cluster, err := ensureClusterLogin(ctx, runner, client, interactive)
		if err != nil {
			return err
		}
		ui.PrintClusterContext("Preflight", cluster.User, cluster.Server, o.namespace, "")
		discovery := discoverInstallDiscovery(ctx, client)
		ui.PrintInstallDiscovery(ui.InstallDiscoveryHints{MASRoutes: discovery.MASRoutes})
		namespace, masBaseURL, err := ui.PromptPreflight(o.namespace, o.masBaseURL, discovery.MASRoutes)
		if err != nil {
			return err
		}
		o.namespace = namespace
		o.masBaseURL = masBaseURL
	} else if _, err := ensureClusterLogin(ctx, runner, client, interactive); err != nil {
		return err
	}

	// API token inputs are optional and env/flag-only — never prompted. Both
	// must be present for the key check to run; a lone name or value is not
	// enough to authenticate.
	//
	// CheckIDPCfgOverwrite is deliberately left off: the IDPCfg names depend on
	// the MAS instance id and the resolved auth provider list, and this command
	// has neither (it takes no --configure-mas-auth / --mas-auth-* inputs).
	// Adding them would mean inventing new required inputs for a command whose
	// job is a quick cluster/MAS reachability check, so the warning fires only
	// from `mas-est install`, where the values are already resolved.
	report := preflight.Run(ctx, client, preflight.Input{
		Namespace:     o.namespace,
		MASBaseURL:    o.masBaseURL,
		APITokenName:  o.masAPITokenName,
		APITokenValue: o.masAPITokenVal,
	})
	preflight.Print(os.Stdout, report)

	if report.HasFailures() {
		return fmt.Errorf("preflight failed")
	}
	return nil
}
