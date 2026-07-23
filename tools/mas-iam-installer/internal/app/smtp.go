package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	executil "github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/exec"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/ui"
)

const (
	defaultMailpitNamespace   = config.DefaultNamespace
	defaultMailpitName        = "mas-mailpit"
	// defaultMailpitImage points at the GitHub Container Registry mirror
	// rather than Docker Hub: GHCR has no anonymous pull rate limit, which
	// matters on clusters that share an egress IP with other workloads
	// (Fyre, shared dev clusters, etc.). Tagged to a specific version so
	// installs are reproducible — bump it when upstream cuts a new release.
	defaultMailpitImage       = "ghcr.io/axllent/mailpit:v1.30.1"
	defaultMailpitSMTPPort    = 1025
	defaultMailpitUIPort      = 8025
	mailpitRelaySecretName    = "mas-est-mailpit-relay"
	mailpitRelayConfigPath    = "/etc/mailpit/relay.yaml"
	mailpitRelayConfigKey     = "relay.yaml"
	mailpitRelayVolumeName    = "mailpit-relay"
	defaultMailpitRelayPort   = 587
	defaultMailpitRelayAuth   = "plain"
)

type mailpitInstallOptions struct {
	namespace string
	name      string
	image     string
	routeHost string
	timeout   time.Duration

	// Relay options. When relayHost is set, Mailpit forwards captured
	// messages to that upstream SMTP server in addition to storing them
	// for the UI. Leave relayHost empty for capture-only (the default
	// lab behaviour).
	relayHost        string
	relayPort        int
	relayUsername    string
	relayPassword    string
	relayFrom        string
	relayAuth        string
	relayStartTLS    bool
	relayAllowedRcpt string
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
	defaults := config.LoadInstallConfigFromEnv()
	opts := &mailpitInstallOptions{
		namespace:        defaultMailpitNamespace,
		name:             defaultMailpitName,
		image:            defaultMailpitImage,
		timeout:          5 * time.Minute,
		relayHost:        defaults.SMTPRelayHost,
		relayPort:        defaults.SMTPRelayPort,
		relayUsername:    defaults.SMTPRelayUsername,
		relayPassword:    defaults.SMTPRelayPassword,
		relayFrom:        defaults.SMTPRelayFrom,
		relayAuth:        defaults.SMTPRelayAuth,
		relayStartTLS:    defaults.SMTPRelayStartTLS,
		relayAllowedRcpt: defaults.SMTPRelayAllowedRcpt,
	}

	command := &cobra.Command{
		Use:   "install-mailpit",
		Short: "Create a Mailpit SMTP capture service (with optional upstream relay)",
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

	// Relay flags. Setting --smtp-relay-host turns on relay; everything
	// else has sensible defaults (port 587 / STARTTLS on / plain auth)
	// that match Gmail, Outlook, SendGrid, and most provider examples.
	flags.StringVar(&opts.relayHost, "smtp-relay-host", opts.relayHost, "Upstream SMTP host to relay captured messages through; leave empty for capture-only")
	flags.IntVar(&opts.relayPort, "smtp-relay-port", opts.relayPort, "Upstream SMTP port (587 for STARTTLS, 465 for implicit TLS, 25 for plain)")
	flags.StringVar(&opts.relayUsername, "smtp-relay-username", opts.relayUsername, "Upstream SMTP username")
	flags.StringVar(&opts.relayPassword, "smtp-relay-password", opts.relayPassword, "Upstream SMTP password (or app password for Gmail)")
	flags.StringVar(&opts.relayFrom, "smtp-relay-from", opts.relayFrom, "Envelope sender address for relayed messages (Return-Path); MAS's From header is preserved separately")
	flags.StringVar(&opts.relayAuth, "smtp-relay-auth", opts.relayAuth, "Upstream auth mechanism: plain, login, cram-md5, or none")
	flags.BoolVar(&opts.relayStartTLS, "smtp-relay-starttls", opts.relayStartTLS, "Use STARTTLS when talking to the upstream relay (recommended on port 587)")
	flags.StringVar(&opts.relayAllowedRcpt, "smtp-relay-allowed-recipients", opts.relayAllowedRcpt, "Optional regex restricting which recipient addresses get relayed; empty = relay everything captured")

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
	if err := applyProviderConnectionConfigMap(ctx, client, o.namespace, smtpConnectionConfigMapName, "smtp", o.smtpConnectionConfigMapData()); err != nil {
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
	o.relayHost = strings.TrimSpace(o.relayHost)
	if o.relayHost != "" {
		if o.relayPort == 0 {
			o.relayPort = defaultMailpitRelayPort
		}
		if strings.TrimSpace(o.relayAuth) == "" {
			o.relayAuth = defaultMailpitRelayAuth
		}
	}
	return nil
}

func (o *mailpitInstallOptions) relayEnabled() bool {
	return strings.TrimSpace(o.relayHost) != ""
}

func (o *mailpitInstallOptions) manifests() []map[string]any {
	manifests := []map[string]any{
		mailpitNamespaceManifest(o),
	}
	if o.relayEnabled() {
		manifests = append(manifests, mailpitRelaySecretManifest(o))
	}
	manifests = append(manifests,
		mailpitServiceManifest(o),
		mailpitDeploymentManifest(o),
		mailpitRouteManifest(o),
	)
	return manifests
}

func (o *mailpitInstallOptions) serviceHost() string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", o.name, o.namespace)
}

func (o *mailpitInstallOptions) smtpURL() string {
	return fmt.Sprintf("smtp://%s:%d", o.serviceHost(), defaultMailpitSMTPPort)
}

func (o *mailpitInstallOptions) details() map[string]string {
	d := map[string]string{
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
	if o.relayEnabled() {
		d["smtp.deliveryMode"] = "capture-and-relay"
		d["smtp.relayHost"] = o.relayHost
		d["smtp.relayPort"] = fmt.Sprintf("%d", o.relayPort)
		d["smtp.relayFrom"] = o.relayFrom
		d["smtp.relayStartTLS"] = boolString(o.relayStartTLS)
		d["smtp.relayCredentialsSecret"] = fmt.Sprintf("%s/%s key %s", o.namespace, mailpitRelaySecretName, mailpitRelayConfigKey)
	}
	return d
}

// smtpConnectionConfigMapData builds the payload for mas-est-smtp-connection
// (a ConfigMap, since Mailpit-side credentials live in their own Secret
// when relay is enabled). All values here are non-sensitive — the upstream
// username/password are NEVER written here; consumers wanting those fetch
// the mas-est-mailpit-relay Secret directly.
func (o *mailpitInstallOptions) smtpConnectionConfigMapData() map[string]string {
	data := map[string]string{
		"host":           o.serviceHost(),
		"port":           fmt.Sprintf("%d", defaultMailpitSMTPPort),
		"from":           "mas-est@example.local",
		"webUI":          "https://" + o.routeHost,
		"tls":            "disabled",
		"authentication": "none",
		"relayEnabled":   boolString(o.relayEnabled()),
	}
	if o.relayEnabled() {
		data["relayHost"] = o.relayHost
		data["relayPort"] = fmt.Sprintf("%d", o.relayPort)
		data["relayFrom"] = o.relayFrom
		data["relayStartTLS"] = boolString(o.relayStartTLS)
		data["relayAuth"] = o.relayAuth
		// IMPORTANT: don't put credentials in this ConfigMap. They live
		// in secret/mas-est-mailpit-relay only.
		data["relayCredentialsSecret"] = mailpitRelaySecretName
	}
	return data
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
	if o.relayEnabled() {
		fmt.Fprintf(os.Stdout, "  Delivery mode: capture + relay to %s:%d (STARTTLS=%t, auth=%s)\n", o.relayHost, o.relayPort, o.relayStartTLS, o.relayAuth)
		fmt.Fprintf(os.Stdout, "  Relay credentials secret: %s/%s key %s\n", o.namespace, mailpitRelaySecretName, mailpitRelayConfigKey)
	} else {
		fmt.Fprintln(os.Stdout, "  Delivery mode: capture-only; messages are not relayed externally")
	}
}

// mailpitRelayConfigYAML renders Mailpit's smtp-relay-config file. The
// schema follows Mailpit upstream docs:
// https://mailpit.axllent.org/docs/configuration/smtp-relay/. We
// hand-render to keep the format byte-stable (deterministic for tests)
// rather than pulling in a YAML library.
func (o *mailpitInstallOptions) mailpitRelayConfigYAML() string {
	auth := strings.TrimSpace(o.relayAuth)
	if auth == "" {
		auth = defaultMailpitRelayAuth
	}
	var b strings.Builder
	fmt.Fprintf(&b, "host: %s\n", o.relayHost)
	fmt.Fprintf(&b, "port: %d\n", o.relayPort)
	fmt.Fprintf(&b, "starttls: %t\n", o.relayStartTLS)
	fmt.Fprintf(&b, "auth: %s\n", auth)
	if o.relayUsername != "" {
		fmt.Fprintf(&b, "username: %s\n", yamlQuote(o.relayUsername))
	}
	if o.relayPassword != "" {
		fmt.Fprintf(&b, "password: %s\n", yamlQuote(o.relayPassword))
	}
	if o.relayFrom != "" {
		fmt.Fprintf(&b, "return-path: %s\n", yamlQuote(o.relayFrom))
	}
	if o.relayAllowedRcpt != "" {
		fmt.Fprintf(&b, "allowed-recipients: %s\n", yamlQuote(o.relayAllowedRcpt))
	}
	return b.String()
}

// yamlQuote wraps a value in YAML double quotes, escaping characters that
// would otherwise break the parser (backslash and double quote). Used for
// any value that might contain colons, special characters, or whitespace.
func yamlQuote(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

func mailpitRelaySecretManifest(o *mailpitInstallOptions) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"type":       "Opaque",
		"metadata": map[string]any{
			"name":      mailpitRelaySecretName,
			"namespace": o.namespace,
			"labels":    mailpitLabels(o),
		},
		"stringData": map[string]string{
			mailpitRelayConfigKey: o.mailpitRelayConfigYAML(),
		},
	}
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
	env := []map[string]any{
		{
			"name":  "MP_SMTP_BIND_ADDR",
			"value": fmt.Sprintf("0.0.0.0:%d", defaultMailpitSMTPPort),
		},
		{
			"name":  "MP_UI_BIND_ADDR",
			"value": fmt.Sprintf("0.0.0.0:%d", defaultMailpitUIPort),
		},
	}
	container := map[string]any{
		"name":  "mailpit",
		"image": o.image,
		"env":   env,
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
	}
	podSpec := map[string]any{
		"containers": []map[string]any{container},
	}

	if o.relayEnabled() {
		// Mailpit reads the relay config from a YAML file pointed at by
		// MP_SMTP_RELAY_CONFIG. We mount the Secret read-only so the
		// credentials never appear on disk in the pod's writable layer
		// or in pod env vars.
		env = append(env, map[string]any{
			"name":  "MP_SMTP_RELAY_CONFIG",
			"value": mailpitRelayConfigPath,
		})
		container["env"] = env
		container["volumeMounts"] = []map[string]any{
			{
				"name":      mailpitRelayVolumeName,
				"mountPath": mailpitRelayConfigPath,
				"subPath":   mailpitRelayConfigKey,
				"readOnly":  true,
			},
		}
		podSpec["volumes"] = []map[string]any{
			{
				"name": mailpitRelayVolumeName,
				"secret": map[string]any{
					"secretName": mailpitRelaySecretName,
					"items": []map[string]any{
						{
							"key":  mailpitRelayConfigKey,
							"path": mailpitRelayConfigKey,
						},
					},
				},
			},
		}
		podSpec["containers"] = []map[string]any{container}
	}

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
				"spec": podSpec,
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
