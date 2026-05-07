package app

import (
	"context"
	"encoding/base64"
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
	scimBridgeConfigMapName = "scim-bridge-config"
	scimBridgeSecretName    = "scim-bridge-secret"
	scimBridgeDeployment    = "deployment/scim-bridge"
	redactedRuntimeValue    = "[REDACTED]"

	masAPITokenNameKey  = "SCIM_BRIDGE_MAS_API_TOKEN_NAME"
	masAPITokenValueKey = "SCIM_BRIDGE_MAS_API_TOKEN_VALUE"
)

type runtimeConfigOptions struct {
	namespace string
}

type runtimeConfigSetMASAPITokenOptions struct {
	namespace  string
	tokenName  string
	tokenValue string
}

func newConfigCommand() *cobra.Command {
	defaults := config.LoadInstallConfigFromEnv()
	opts := &runtimeConfigOptions{
		namespace: defaults.Namespace,
	}

	command := &cobra.Command{
		Use:   "config",
		Short: "View and update SCIM bridge runtime configuration",
	}

	viewCommand := &cobra.Command{
		Use:   "view",
		Short: "Show SCIM bridge runtime configuration with secrets redacted",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.runView(cmd.Context())
		},
	}
	viewCommand.Flags().StringVar(&opts.namespace, "namespace", opts.namespace, "Target namespace")

	command.AddCommand(viewCommand, newConfigSetCommand(defaults.Namespace))
	return command
}

func newConfigSetCommand(defaultNamespace string) *cobra.Command {
	command := &cobra.Command{
		Use:   "set",
		Short: "Update SCIM bridge runtime configuration",
	}

	masAPITokenOpts := &runtimeConfigSetMASAPITokenOptions{
		namespace: defaultNamespace,
	}
	masAPITokenCommand := &cobra.Command{
		Use:   "mas-api-token",
		Short: "Rotate the SCIM bridge MAS API token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return masAPITokenOpts.run(cmd.Context())
		},
	}
	flags := masAPITokenCommand.Flags()
	flags.StringVar(&masAPITokenOpts.namespace, "namespace", masAPITokenOpts.namespace, "Target namespace")
	flags.StringVar(&masAPITokenOpts.tokenName, "token-name", "", "New MAS API token name")
	flags.StringVar(&masAPITokenOpts.tokenValue, "token-value", "", "New MAS API token value")

	command.AddCommand(masAPITokenCommand)
	return command
}

func (o *runtimeConfigOptions) runView(ctx context.Context) error {
	runner := executil.NewRunner()
	client := oc.NewClient(runner)
	if _, err := ensureClusterLogin(ctx, runner, client, ui.IsInteractive()); err != nil {
		return err
	}

	namespace := defaultedNamespace(o.namespace)
	configData, err := client.ConfigMapData(ctx, namespace, scimBridgeConfigMapName)
	if err != nil {
		return fmt.Errorf("read configmap/%s in namespace %s: %w", scimBridgeConfigMapName, namespace, err)
	}
	secretKeys, err := client.SecretKeys(ctx, namespace, scimBridgeSecretName)
	if err != nil {
		return fmt.Errorf("read secret/%s in namespace %s: %w", scimBridgeSecretName, namespace, err)
	}

	fmt.Fprintln(os.Stdout, "SCIM bridge runtime config")
	fmt.Fprintf(os.Stdout, "Namespace: %s\n\n", namespace)

	fmt.Fprintf(os.Stdout, "configmap/%s\n", scimBridgeConfigMapName)
	printRuntimeConfigMap(os.Stdout, configData)
	fmt.Fprintln(os.Stdout)

	fmt.Fprintf(os.Stdout, "secret/%s (values redacted)\n", scimBridgeSecretName)
	printRuntimeSecretKeys(os.Stdout, secretKeys)
	return nil
}

func (o *runtimeConfigSetMASAPITokenOptions) run(ctx context.Context) error {
	if strings.TrimSpace(o.tokenName) == "" {
		return fmt.Errorf("--token-name is required")
	}
	if o.tokenValue == "" {
		return fmt.Errorf("--token-value is required")
	}

	runner := executil.NewRunner()
	client := oc.NewClient(runner)
	if _, err := ensureClusterLogin(ctx, runner, client, ui.IsInteractive()); err != nil {
		return err
	}

	namespace := defaultedNamespace(o.namespace)
	patch, err := masAPITokenSecretMergePatch(strings.TrimSpace(o.tokenName), o.tokenValue)
	if err != nil {
		return err
	}

	if _, err := client.Patch(ctx, namespace, "secret/"+scimBridgeSecretName, patch); err != nil {
		return fmt.Errorf("update secret/%s in namespace %s: %w", scimBridgeSecretName, namespace, err)
	}
	fmt.Fprintf(os.Stdout, "Updated secret/%s in namespace %s keys: %s, %s\n", scimBridgeSecretName, namespace, masAPITokenNameKey, masAPITokenValueKey)

	if _, err := client.RolloutRestart(ctx, namespace, scimBridgeDeployment); err != nil {
		return fmt.Errorf("restart %s in namespace %s: %w", scimBridgeDeployment, namespace, err)
	}
	fmt.Fprintf(os.Stdout, "Restarted %s in namespace %s\n", scimBridgeDeployment, namespace)

	if _, err := client.RolloutStatus(ctx, namespace, scimBridgeDeployment, "5m"); err != nil {
		return fmt.Errorf("wait for %s rollout in namespace %s: %w", scimBridgeDeployment, namespace, err)
	}
	fmt.Fprintf(os.Stdout, "Rollout complete for %s in namespace %s\n", scimBridgeDeployment, namespace)
	return nil
}

func defaultedNamespace(namespace string) string {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return config.DefaultNamespace
	}
	return namespace
}

func printRuntimeConfigMap(w io.Writer, data map[string]string) {
	if len(data) == 0 {
		fmt.Fprintln(w, "  none")
		return
	}
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(w, "  %s=%s\n", key, runtimeConfigDisplayValue(key, data[key], false))
	}
}

func printRuntimeSecretKeys(w io.Writer, keys []string) {
	if len(keys) == 0 {
		fmt.Fprintln(w, "  none")
		return
	}
	for _, key := range keys {
		fmt.Fprintf(w, "  %s=%s\n", key, runtimeConfigDisplayValue(key, "", true))
	}
}

func runtimeConfigDisplayValue(key, value string, secret bool) string {
	if secret || isSensitiveSecretKey(key) {
		return redactedRuntimeValue
	}
	return value
}

func masAPITokenSecretMergePatch(tokenName, tokenValue string) ([]byte, error) {
	patch := struct {
		Data map[string]string `json:"data"`
	}{
		Data: map[string]string{
			masAPITokenNameKey:  base64.StdEncoding.EncodeToString([]byte(tokenName)),
			masAPITokenValueKey: base64.StdEncoding.EncodeToString([]byte(tokenValue)),
		},
	}

	raw, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("encode secret/%s patch: %w", scimBridgeSecretName, err)
	}
	return raw, nil
}
