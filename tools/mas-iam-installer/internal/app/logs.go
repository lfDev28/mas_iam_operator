package app

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	executil "github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/exec"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/ui"
)

type logsOptions struct {
	namespace string
	component string
	tail      int
	follow    bool
}

func newLogsCommand() *cobra.Command {
	opts := &logsOptions{
		namespace: config.LoadInstallConfigFromEnv().Namespace,
		component: "bridge",
		tail:      200,
	}

	command := &cobra.Command{
		Use:   "logs",
		Short: "Show logs for MAS EST IAM components",
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.run(cmd.Context())
		},
	}

	flags := command.Flags()
	flags.StringVar(&opts.namespace, "namespace", opts.namespace, "Target namespace")
	flags.StringVar(&opts.component, "component", opts.component, "Log target: operator, keycloak, openldap, bridge, profile-bootstrap, minio, minio-init, or smtp")
	flags.IntVar(&opts.tail, "tail", opts.tail, "Number of log lines to show")
	flags.BoolVar(&opts.follow, "follow", false, "Follow logs")

	return command
}

func (o *logsOptions) run(ctx context.Context) error {
	runner := executil.NewRunner()
	client := oc.NewClient(runner)

	args, err := o.componentArgs()
	if err != nil {
		return err
	}
	if _, err := ensureClusterLogin(ctx, runner, client, ui.IsInteractive()); err != nil {
		return err
	}
	return client.Logs(ctx, o.namespace, args, os.Stdout, os.Stderr)
}

func (o *logsOptions) componentArgs() ([]string, error) {
	args := []string{}
	switch o.component {
	case "operator":
		args = append(args, "deployment/mas-iam-operator-controller-manager")
	case "keycloak":
		args = append(args, "deployment/mas-est-iam")
	case "openldap":
		args = append(args, "deployment/mas-est-iam-openldap")
	case "bridge":
		args = append(args, "deployment/scim-bridge")
	case "profile-bootstrap":
		args = append(args, "job/scim-bridge-mas-profile-bootstrap")
	case "minio":
		args = append(args, "deployment/mas-minio")
	case "minio-init":
		args = append(args, "job/mas-minio-bucket-init")
	case "smtp":
		args = append(args, "deployment/mas-mailpit")
	default:
		return nil, fmt.Errorf("unknown component %q (expected operator, keycloak, openldap, bridge, profile-bootstrap, minio, minio-init, or smtp)", o.component)
	}

	args = append(args, fmt.Sprintf("--tail=%d", o.tail))
	if o.follow {
		args = append(args, "-f")
	}
	return args, nil
}
