package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	executil "github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/exec"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/ui"
)

type statusOptions struct {
	namespace string
}

func newStatusCommand() *cobra.Command {
	opts := &statusOptions{
		namespace: config.LoadInstallConfigFromEnv().Namespace,
	}

	command := &cobra.Command{
		Use:   "status",
		Short: "Show the current health of MAS IAM components",
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.run(cmd.Context())
		},
	}

	command.Flags().StringVar(&opts.namespace, "namespace", opts.namespace, "Target namespace")
	return command
}

func (o *statusOptions) run(ctx context.Context) error {
	runner := executil.NewRunner()
	client := oc.NewClient(runner)
	cluster, err := ensureClusterLogin(ctx, runner, client, ui.IsInteractive())
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "Cluster: %s @ %s\n", cluster.User, cluster.Server)
	fmt.Fprintf(os.Stdout, "Namespace: %s\n", o.namespace)

	csv, err := client.CSVPhase(ctx, o.namespace)
	printStatusLine("CSV", err, fmt.Sprintf("%s phase=%s", csv.Name, csv.Phase))

	operator, err := client.Deployment(ctx, o.namespace, "mas-iam-operator-controller-manager")
	printStatusLine("Operator controller", err, deploymentSummary(operator))

	keycloak, err := client.Deployment(ctx, o.namespace, "mas-iam-sample")
	printStatusLine("IAM core", err, deploymentSummary(keycloak))

	openldap, err := client.Deployment(ctx, o.namespace, "mas-iam-sample-openldap")
	printStatusLine("OpenLDAP", err, deploymentSummary(openldap))
	if err == nil {
		fmt.Fprintf(os.Stdout, "LDAP details: mas-iam ldap-info --namespace %s\n", o.namespace)
	}

	postgres, err := client.Pod(ctx, o.namespace, "mas-iam-sample-postgresql-0")
	printStatusLine("PostgreSQL", err, podSummary(postgres))

	bridge, err := client.Deployment(ctx, o.namespace, "scim-bridge")
	printStatusLine("SCIM bridge", err, deploymentSummary(bridge))

	fmt.Fprintln(os.Stdout, "Jobs:")
	for _, jobSpec := range []struct {
		name        string
		missingNote string
	}{
		{
			name:        "mas-iam-sample-generate-openldap-tls",
			missingNote: "ttl-cleaned after completion",
		},
		{
			name: "mas-iam-sample-ldap-config",
		},
		{
			name:        "scim-bridge-keycloak-bootstrap",
			missingNote: "not created when keycloak bootstrap mode=script",
		},
		{
			name:        "scim-bridge-keycloak-route-cert",
			missingNote: "not created when route cert automation is disabled",
		},
		{
			name: "scim-bridge-mas-profile-bootstrap",
		},
	} {
		job, err := client.Job(ctx, o.namespace, jobSpec.name)
		printIndentedStatusLine(jobSpec.name, err, jobSummary(job), jobSpec.missingNote)
	}

	fmt.Fprintln(os.Stdout, "PVCs:")
	pvcs, err := client.PVCs(ctx, o.namespace)
	if err != nil {
		printIndentedStatusLine("pvc-list", err, "", "")
		return nil
	}
	if len(pvcs) == 0 {
		fmt.Fprintln(os.Stdout, "  none")
		return nil
	}
	for _, pvc := range pvcs {
		fmt.Fprintf(os.Stdout, "  %s: phase=%s storageClass=%s\n", pvc.Name, pvc.Phase, pvc.StorageClass)
	}

	return nil
}

func deploymentSummary(status oc.DeploymentStatus) string {
	return fmt.Sprintf("ready=%d/%d available=%d", status.ReadyReplicas, status.DesiredReplicas, status.AvailableReplicas)
}

func podSummary(status oc.PodStatus) string {
	return fmt.Sprintf("phase=%s ready=%d/%d", status.Phase, status.ReadyContainers, status.TotalContainers)
}

func jobSummary(status oc.JobStatus) string {
	return fmt.Sprintf("complete=%t active=%d succeeded=%d failed=%d", status.Complete, status.Active, status.Succeeded, status.Failed)
}

func printStatusLine(label string, err error, summary string) {
	if err != nil {
		if oc.IsNotFound(err) {
			fmt.Fprintf(os.Stdout, "%s: missing\n", label)
			return
		}
		fmt.Fprintf(os.Stdout, "%s: error: %v\n", label, err)
		return
	}
	fmt.Fprintf(os.Stdout, "%s: %s\n", label, summary)
}

func printIndentedStatusLine(label string, err error, summary, missingNote string) {
	if err != nil {
		if oc.IsNotFound(err) {
			if missingNote != "" {
				fmt.Fprintf(os.Stdout, "  %s: absent (%s)\n", label, missingNote)
			} else {
				fmt.Fprintf(os.Stdout, "  %s: missing\n", label)
			}
			return
		}
		fmt.Fprintf(os.Stdout, "  %s: error: %v\n", label, sanitizeWhitespace(err.Error()))
		return
	}
	fmt.Fprintf(os.Stdout, "  %s: %s\n", label, summary)
}

func sanitizeWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
