package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	executil "github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/exec"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/ui"
)

const (
	connectionDetailsSecretName = "mas-est-connection-details"
	ldapConnectionSecretName    = "mas-est-ldap-connection"
	oidcConnectionSecretName    = "mas-est-oidc-connection"
	samlConnectionSecretName    = "mas-est-saml-connection"
	s3ConnectionSecretName      = "mas-est-s3-connection"
	smtpConnectionConfigMapName = "mas-est-smtp-connection"
)

type detailsOptions struct {
	namespace   string
	component   string
	showSecrets bool
}

func newDetailsCommand() *cobra.Command {
	defaults := config.LoadInstallConfigFromEnv()
	opts := &detailsOptions{
		namespace: defaults.Namespace,
		component: "all",
	}

	command := &cobra.Command{
		Use:   "details",
		Short: "Show MAS EST connection details",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.run(cmd.Context())
		},
	}
	flags := command.Flags()
	flags.StringVar(&opts.namespace, "namespace", opts.namespace, "Target namespace")
	flags.StringVar(&opts.component, "component", opts.component, "Component to show: ldap, oidc, saml, s3, smtp, or all")
	flags.BoolVar(&opts.showSecrets, "show-secrets", false, "Show sensitive values stored in details secrets")
	return command
}

func (o *detailsOptions) run(ctx context.Context) error {
	runner := executil.NewRunner()
	client := oc.NewClient(runner)
	if _, err := ensureClusterLogin(ctx, runner, client, ui.IsInteractive()); err != nil {
		return err
	}

	namespace := defaultedNamespace(o.namespace)
	data, err := client.SecretData(ctx, namespace, connectionDetailsSecretName)
	if err != nil {
		return fmt.Errorf("read secret/%s in namespace %s: %w", connectionDetailsSecretName, namespace, err)
	}

	component := strings.ToLower(strings.TrimSpace(o.component))
	if component == "" {
		component = "all"
	}
	if component != "all" && !knownDetailsComponent(component) {
		return fmt.Errorf("--component must be ldap, oidc, saml, s3, smtp, or all")
	}

	fmt.Fprintf(os.Stdout, "MAS EST connection details\nNamespace: %s\nSecret: %s\n\n", namespace, connectionDetailsSecretName)
	if component == "all" {
		for _, name := range []string{"ldap", "oidc", "saml", "s3", "smtp"} {
			printDetailsComponent(os.Stdout, name, data, o.showSecrets)
		}
		return nil
	}
	printDetailsComponent(os.Stdout, component, data, o.showSecrets)
	return nil
}

func knownDetailsComponent(component string) bool {
	switch component {
	case "ldap", "oidc", "saml", "s3", "smtp":
		return true
	default:
		return false
	}
}

func printDetailsComponent(out io.Writer, component string, data map[string]string, showSecrets bool) {
	prefix := component + "."
	keys := []string{}
	for key := range data {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		fmt.Fprintf(out, "%s: not configured\n\n", strings.ToUpper(component))
		return
	}
	sort.Strings(keys)
	fmt.Fprintf(out, "%s\n", strings.ToUpper(component))
	for _, key := range keys {
		value := data[key]
		if !showSecrets && isSensitiveSecretKey(key) {
			value = redactedRuntimeValue
		}
		fmt.Fprintf(out, "  %s: %s\n", strings.TrimPrefix(key, prefix), value)
	}
	fmt.Fprintln(out)
}

// applyProviderConnectionSecret writes (server-side applies) an Opaque Secret
// with the given stringData. Used to expose per-provider connection details
// (LDAP, OIDC, SAML, S3) so support engineers can `oc get secret mas-est-X-connection`
// without trawling configmaps, IDPCfg CRs, and Keycloak admin in turn.
func applyProviderConnectionSecret(ctx context.Context, client *oc.Client, namespace, name, provider string, data map[string]string) error {
	if len(data) == 0 {
		return nil
	}
	manifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
			"labels": map[string]string{
				"app.kubernetes.io/managed-by":        "mas-est-installer",
				"mas-est.ibm.com/connection-provider": provider,
			},
		},
		"type":       "Opaque",
		"stringData": data,
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if _, err := client.Apply(ctx, raw); err != nil {
		return fmt.Errorf("apply secret/%s in namespace %s: %w", name, namespace, err)
	}
	return nil
}

// applyProviderConnectionConfigMap mirrors applyProviderConnectionSecret for
// the SMTP/Mailpit case where there is no authentication and therefore no
// sensitive data; a ConfigMap is the correct shape.
func applyProviderConnectionConfigMap(ctx context.Context, client *oc.Client, namespace, name, provider string, data map[string]string) error {
	if len(data) == 0 {
		return nil
	}
	manifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
			"labels": map[string]string{
				"app.kubernetes.io/managed-by":        "mas-est-installer",
				"mas-est.ibm.com/connection-provider": provider,
			},
		},
		"data": data,
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if _, err := client.Apply(ctx, raw); err != nil {
		return fmt.Errorf("apply configmap/%s in namespace %s: %w", name, namespace, err)
	}
	return nil
}

func applyConnectionDetails(ctx context.Context, client *oc.Client, namespace string, updates map[string]string) error {
	if len(updates) == 0 {
		return nil
	}
	existing, err := client.SecretData(ctx, namespace, connectionDetailsSecretName)
	if err != nil && !oc.IsNotFound(err) {
		return err
	}
	if existing == nil {
		existing = map[string]string{}
	}
	for key, value := range updates {
		existing[key] = value
	}
	manifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      connectionDetailsSecretName,
			"namespace": namespace,
			"labels": map[string]string{
				"app.kubernetes.io/managed-by": "mas-est-installer",
			},
		},
		"type":       "Opaque",
		"stringData": existing,
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if _, err := client.Apply(ctx, raw); err != nil {
		return fmt.Errorf("apply secret/%s in namespace %s: %w", connectionDetailsSecretName, namespace, err)
	}
	return nil
}
