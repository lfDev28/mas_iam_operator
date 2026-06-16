package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	executil "github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/exec"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/ui"
)

const (
	defaultMailpitNamespace = config.DefaultNamespace
	defaultMailpitName      = "mas-mailpit"
	// defaultMailpitImage points at the GitHub Container Registry mirror
	// rather than Docker Hub: GHCR has no anonymous pull rate limit, which
	// matters on clusters that share an egress IP with other workloads
	// (Fyre, shared dev clusters, etc.). Tagged to a specific version so
	// installs are reproducible — bump it when upstream cuts a new release.
	defaultMailpitImage    = "ghcr.io/axllent/mailpit:v1.30.1"
	defaultMailpitSMTPPort = 1025
	defaultMailpitUIPort   = 8025
)

type mailpitInstallOptions struct {
	namespace string
	name      string
	image     string
	routeHost string
	timeout   time.Duration
}

func newSMTPCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "smtp",
		Short: "Install and inspect MAS SMTP helpers",
	}
	command.AddCommand(newSMTPInstallMailpitCommand())
	return command
}

func newSMTPInstallMailpitCommand() *cobra.Command {
	opts := &mailpitInstallOptions{
		namespace: defaultMailpitNamespace,
		name:      defaultMailpitName,
		image:     defaultMailpitImage,
		timeout:   5 * time.Minute,
	}

	command := &cobra.Command{
		Use:   "install-mailpit",
		Short: "Create a Mailpit SMTP capture service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.run(cmd.Context())
		},
	}

	flags := command.Flags()
	flags.StringVar(&opts.namespace, "namespace", opts.namespace, "Namespace to install Mailpit into")
	flags.StringVar(&opts.name, "name", opts.name, "Mailpit resource name prefix")
	flags.StringVar(&opts.image, "image", opts.image, "Mailpit image")
	flags.StringVar(&opts.routeHost, "route-host", opts.routeHost, "External Mailpit UI route host")
	flags.DurationVar(&opts.timeout, "timeout", opts.timeout, "Time to wait for Mailpit readiness")

	return command
}

func (o *mailpitInstallOptions) run(ctx context.Context) error {
	runner := executil.NewRunner()
	client := oc.NewClient(runner)
	if _, err := ensureClusterLogin(ctx, runner, client, ui.IsInteractive()); err != nil {
		return err
	}
	if err := o.defaultValues(ctx); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "[smtp] installing Mailpit %s in namespace %s\n", o.name, o.namespace)
	for _, manifest := range o.manifests() {
		if err := applyObjectStorageManifest(ctx, client, manifest); err != nil {
			return err
		}
	}
	if _, err := client.RolloutStatus(ctx, o.namespace, "deployment/"+o.name, o.timeout.String()); err != nil {
		return fmt.Errorf("wait for deployment/%s in namespace %s: %w", o.name, o.namespace, err)
	}
	if err := applyConnectionDetails(ctx, client, o.namespace, o.details()); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "[smtp] Mailpit rollout complete: %s/deployment/%s\n", o.namespace, o.name)
	o.printSummary()
	return nil
}

func (o *mailpitInstallOptions) defaultValues(ctx context.Context) error {
	o.namespace = defaultString(o.namespace, defaultMailpitNamespace)
	o.name = defaultString(o.name, defaultMailpitName)
	o.image = defaultString(o.image, defaultMailpitImage)
	if o.timeout == 0 {
		o.timeout = 5 * time.Minute
	}
	if o.routeHost == "" {
		host, err := defaultObjectStorageRouteHost(ctx, o.name)
		if err != nil {
			return err
		}
		o.routeHost = host
	}
	return nil
}

func (o *mailpitInstallOptions) manifests() []map[string]any {
	return []map[string]any{
		mailpitNamespaceManifest(o),
		mailpitServiceManifest(o),
		mailpitDeploymentManifest(o),
		mailpitRouteManifest(o),
	}
}

func (o *mailpitInstallOptions) serviceHost() string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", o.name, o.namespace)
}

func (o *mailpitInstallOptions) smtpURL() string {
	return fmt.Sprintf("smtp://%s:%d", o.serviceHost(), defaultMailpitSMTPPort)
}

func (o *mailpitInstallOptions) details() map[string]string {
	return map[string]string{
		"smtp.displayName":    "MAS EST SMTP Capture",
		"smtp.host":           o.serviceHost(),
		"smtp.port":           fmt.Sprintf("%d", defaultMailpitSMTPPort),
		"smtp.tls":            "disabled",
		"smtp.authentication": "none",
		"smtp.url":            o.smtpURL(),
		"smtp.uiURL":          "https://" + o.routeHost,
		"smtp.deliveryMode":   "capture-only",
		"smtp.deployment":     o.name,
		"smtp.namespace":      o.namespace,
		"smtp.detailsCommand": fmt.Sprintf("mas-est details --namespace %s --component smtp", o.namespace),
	}
}

func (o *mailpitInstallOptions) printSummary() {
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Mailpit SMTP capture details")
	fmt.Fprintf(os.Stdout, "  Display name: MAS EST SMTP Capture\n")
	fmt.Fprintf(os.Stdout, "  SMTP host: %s\n", o.serviceHost())
	fmt.Fprintf(os.Stdout, "  SMTP port: %d\n", defaultMailpitSMTPPort)
	fmt.Fprintln(os.Stdout, "  SMTP security/TLS: disabled")
	fmt.Fprintln(os.Stdout, "  SMTP authentication: none")
	fmt.Fprintf(os.Stdout, "  SMTP URL: %s\n", o.smtpURL())
	fmt.Fprintf(os.Stdout, "  Mailpit UI URL: https://%s\n", o.routeHost)
	fmt.Fprintln(os.Stdout, "  Delivery mode: capture-only; messages are not relayed externally")
}

func mailpitNamespaceManifest(o *mailpitInstallOptions) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name": o.namespace,
			"labels": map[string]string{
				"app.kubernetes.io/managed-by": "mas-est-installer",
			},
		},
	}
}

func mailpitServiceManifest(o *mailpitInstallOptions) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":      o.name,
			"namespace": o.namespace,
			"labels":    mailpitLabels(o),
		},
		"spec": map[string]any{
			"type": "ClusterIP",
			"selector": map[string]string{
				"app.kubernetes.io/instance": o.name,
				"app.kubernetes.io/name":     "mailpit",
			},
			"ports": []map[string]any{
				{
					"name":       "smtp",
					"port":       defaultMailpitSMTPPort,
					"protocol":   "TCP",
					"targetPort": "smtp",
				},
				{
					"name":       "ui",
					"port":       defaultMailpitUIPort,
					"protocol":   "TCP",
					"targetPort": "ui",
				},
			},
		},
	}
}

func mailpitDeploymentManifest(o *mailpitInstallOptions) map[string]any {
	labels := mailpitLabels(o)
	return map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      o.name,
			"namespace": o.namespace,
			"labels":    labels,
		},
		"spec": map[string]any{
			"replicas": 1,
			"selector": map[string]any{
				"matchLabels": map[string]string{
					"app.kubernetes.io/instance": o.name,
					"app.kubernetes.io/name":     "mailpit",
				},
			},
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": labels,
				},
				"spec": map[string]any{
					"containers": []map[string]any{
						{
							"name":  "mailpit",
							"image": o.image,
							"env": []map[string]any{
								{
									"name":  "MP_SMTP_BIND_ADDR",
									"value": fmt.Sprintf("0.0.0.0:%d", defaultMailpitSMTPPort),
								},
								{
									"name":  "MP_UI_BIND_ADDR",
									"value": fmt.Sprintf("0.0.0.0:%d", defaultMailpitUIPort),
								},
							},
							"ports": []map[string]any{
								{
									"name":          "smtp",
									"containerPort": defaultMailpitSMTPPort,
									"protocol":      "TCP",
								},
								{
									"name":          "ui",
									"containerPort": defaultMailpitUIPort,
									"protocol":      "TCP",
								},
							},
						},
					},
				},
			},
		},
	}
}

func mailpitRouteManifest(o *mailpitInstallOptions) map[string]any {
	return map[string]any{
		"apiVersion": "route.openshift.io/v1",
		"kind":       "Route",
		"metadata": map[string]any{
			"name":      o.name,
			"namespace": o.namespace,
			"labels":    mailpitLabels(o),
		},
		"spec": map[string]any{
			"host": o.routeHost,
			"to": map[string]string{
				"kind": "Service",
				"name": o.name,
			},
			"port": map[string]string{
				"targetPort": "ui",
			},
			"tls": map[string]string{
				"termination":                   "edge",
				"insecureEdgeTerminationPolicy": "Redirect",
			},
		},
	}
}

func mailpitLabels(o *mailpitInstallOptions) map[string]string {
	return map[string]string{
		"app.kubernetes.io/instance":   o.name,
		"app.kubernetes.io/managed-by": "mas-est-installer",
		"app.kubernetes.io/name":       "mailpit",
	}
}
