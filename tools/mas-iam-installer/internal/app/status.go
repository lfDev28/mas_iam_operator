package app

import (
	"context"
	"fmt"
	"io"
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

	printMASIAMStatus(os.Stdout, ctx, client, o.namespace, cluster)

	return nil
}

func printMASIAMStatus(w io.Writer, ctx context.Context, client *oc.Client, namespace string, cluster clusterContext) {
	fmt.Fprintf(w, "Cluster: %s @ %s\n", cluster.User, cluster.Server)
	fmt.Fprintf(w, "Namespace: %s\n", namespace)

	csv, err := client.CSVPhase(ctx, namespace)
	printStatusLine(w, "CSV", err, fmt.Sprintf("%s phase=%s", csv.Name, csv.Phase))

	operator, err := client.Deployment(ctx, namespace, "mas-iam-operator-controller-manager")
	printStatusLine(w, "Operator controller", err, deploymentSummary(operator))

	keycloak, err := client.Deployment(ctx, namespace, "mas-iam-sample")
	printStatusLine(w, "IAM core", err, deploymentSummary(keycloak))

	openldap, err := client.Deployment(ctx, namespace, "mas-iam-sample-openldap")
	printStatusLine(w, "OpenLDAP", err, deploymentSummary(openldap))
	if err == nil {
		fmt.Fprintf(w, "LDAP details: mas-iam ldap-info --namespace %s\n", namespace)
	}

	postgres, err := client.Pod(ctx, namespace, "mas-iam-sample-postgresql-0")
	printStatusLine(w, "PostgreSQL", err, podSummary(postgres))

	bridge, err := client.Deployment(ctx, namespace, "scim-bridge")
	printStatusLine(w, "SCIM bridge", err, deploymentSummary(bridge))

	fmt.Fprintln(w, "Jobs:")
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
		job, err := client.Job(ctx, namespace, jobSpec.name)
		printIndentedStatusLine(w, jobSpec.name, err, jobSummary(job), jobSpec.missingNote)
	}

	fmt.Fprintln(w, "PVCs:")
	pvcs, err := client.PVCs(ctx, namespace)
	if err != nil {
		printIndentedStatusLine(w, "pvc-list", err, "", "")
		return
	}
	if len(pvcs) == 0 {
		fmt.Fprintln(w, "  none")
		return
	}
	for _, pvc := range pvcs {
		fmt.Fprintf(w, "  %s: phase=%s storageClass=%s\n", pvc.Name, pvc.Phase, pvc.StorageClass)
	}
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

func printStatusLine(w io.Writer, label string, err error, summary string) {
	if err != nil {
		if oc.IsNotFound(err) {
			fmt.Fprintf(w, "%s: missing\n", label)
			return
		}
		fmt.Fprintf(w, "%s: error: %v\n", label, err)
		return
	}
	fmt.Fprintf(w, "%s: %s\n", label, summary)
}

func printIndentedStatusLine(w io.Writer, label string, err error, summary, missingNote string) {
	if err != nil {
		if oc.IsNotFound(err) {
			if missingNote != "" {
				fmt.Fprintf(w, "  %s: absent (%s)\n", label, missingNote)
			} else {
				fmt.Fprintf(w, "  %s: missing\n", label)
			}
			return
		}
		fmt.Fprintf(w, "  %s: error: %v\n", label, sanitizeWhitespace(err.Error()))
		return
	}
	fmt.Fprintf(w, "  %s: %s\n", label, summary)
}

func sanitizeWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
