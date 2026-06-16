package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	executil "github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/exec"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/masadmin"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/ui"
)

type masAuthDeleteOptions struct {
	namespace      string
	masInstanceID  string
	providers      []string
	ldapProviderID string
	oidcProviderID string
	samlProviderID string
	apiBaseURL     string
	apiTokenName   string
	apiTokenValue  string
	insecureTLS    bool
	ignoreMissing  bool
}

func newMASAuthDeleteCommand() *cobra.Command {
	defaults := config.LoadInstallConfigFromEnv()
	opts := &masAuthDeleteOptions{
		namespace:      defaults.Namespace,
		providers:      defaults.MASAuthProviders,
		ldapProviderID: defaultMASAuthLDAPID,
		oidcProviderID: defaultMASAuthOIDCID,
		samlProviderID: defaultMASAuthSAMLID,
		apiBaseURL:     defaults.MASBaseURL,
		apiTokenName:   defaults.MASAPITokenName,
		apiTokenValue:  defaults.MASAPITokenValue,
		insecureTLS:    true,
		ignoreMissing:  true,
	}

	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete MAS LDAP, OIDC, and SAML IDP configurations via the MAS Admin API",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.run(cmd.Context())
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.namespace, "namespace", opts.namespace, "MAS EST namespace (used to discover MAS API token if not supplied)")
	flags.StringVar(&opts.masInstanceID, "mas-instance-id", opts.masInstanceID, "MAS instance ID (for api route auto-discovery)")
	flags.StringSliceVar(&opts.providers, "providers", opts.providers, "Comma-separated providers to delete: ldap,oidc,saml")
	flags.StringVar(&opts.ldapProviderID, "ldap-provider-id", opts.ldapProviderID, "MAS LDAP provider ID to delete")
	flags.StringVar(&opts.oidcProviderID, "oidc-provider-id", opts.oidcProviderID, "MAS OIDC provider ID to delete")
	flags.StringVar(&opts.samlProviderID, "saml-provider-id", opts.samlProviderID, "MAS SAML provider ID to delete")
	flags.StringVar(&opts.apiBaseURL, "mas-api-base-url", opts.apiBaseURL, "MAS API base URL; auto-discovered from MAS routes when omitted")
	flags.StringVar(&opts.apiTokenName, "mas-api-token-name", opts.apiTokenName, "MAS API key name; read from scim-bridge-secret when omitted")
	flags.StringVar(&opts.apiTokenValue, "mas-api-token-value", opts.apiTokenValue, "MAS API key value; read from scim-bridge-secret when omitted")
	flags.BoolVar(&opts.insecureTLS, "insecure-tls", opts.insecureTLS, "Skip TLS verification when talking to the MAS API")
	flags.BoolVar(&opts.ignoreMissing, "ignore-missing", opts.ignoreMissing, "Do not fail when a provider does not exist (HTTP 404)")
	return cmd
}

func (o *masAuthDeleteOptions) run(ctx context.Context) error {
	runner := executil.NewRunner()
	client := oc.NewClient(runner)
	if _, err := ensureClusterLogin(ctx, runner, client, ui.IsInteractive()); err != nil {
		return err
	}

	providers, err := config.NormalizeMASAuthProviders(o.providers)
	if err != nil {
		return err
	}
	if len(providers) == 0 {
		providers = config.DefaultMASAuthProviders()
	}
	o.providers = providers

	o.namespace = defaultString(o.namespace, config.DefaultNamespace)

	// Resolve API base URL via route discovery if not supplied.
	if strings.TrimSpace(o.apiBaseURL) == "" && strings.TrimSpace(o.masInstanceID) != "" {
		routes, err := client.MASRoutes(ctx)
		if err == nil {
			for _, route := range routes {
				if route.InstanceID == o.masInstanceID {
					o.apiBaseURL = "https://" + route.Host
					break
				}
			}
		}
	}

	// Fall back to scim-bridge-secret for the API token.
	if strings.TrimSpace(o.apiTokenName) == "" || strings.TrimSpace(o.apiTokenValue) == "" {
		data, err := client.SecretData(ctx, o.namespace, scimBridgeSecretName)
		if err == nil {
			if o.apiTokenName == "" {
				o.apiTokenName = data[config.EnvMASAPITokenName]
			}
			if o.apiTokenValue == "" {
				o.apiTokenValue = data[config.EnvMASAPITokenValue]
			}
		} else if !oc.IsNotFound(err) {
			return fmt.Errorf("read MAS API token from secret %s/%s: %w", o.namespace, scimBridgeSecretName, err)
		}
	}
	if strings.TrimSpace(o.apiBaseURL) == "" {
		return fmt.Errorf("--mas-api-base-url is required (set the flag or --mas-instance-id for route discovery)")
	}
	if strings.TrimSpace(o.apiTokenName) == "" || strings.TrimSpace(o.apiTokenValue) == "" {
		return fmt.Errorf("--mas-api-token-name and --mas-api-token-value are required (set the flags or store them in secret %s/%s)", o.namespace, scimBridgeSecretName)
	}

	clientOpts := []masadmin.Option{}
	if o.insecureTLS {
		clientOpts = append(clientOpts, masadmin.WithInsecureTLS())
	}
	mc, err := masadmin.NewClient(o.apiBaseURL, o.apiTokenName, o.apiTokenValue, clientOpts...)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "[mas-auth] using MAS Admin API at %s\n", mc.BaseURL())

	type deletion struct {
		kind   string
		idpId  string
		delete func(context.Context) error
	}
	jobs := []deletion{}
	for _, provider := range providers {
		switch provider {
		case config.MASAuthProviderLDAP:
			jobs = append(jobs, deletion{"ldap", o.ldapProviderID, func(ctx context.Context) error { return mc.DeleteLDAP(ctx, o.ldapProviderID) }})
		case config.MASAuthProviderOIDC:
			jobs = append(jobs, deletion{"oidc", o.oidcProviderID, func(ctx context.Context) error { return mc.DeleteOIDC(ctx, o.oidcProviderID) }})
		case config.MASAuthProviderSAML:
			jobs = append(jobs, deletion{"saml", o.samlProviderID, func(ctx context.Context) error { return mc.DeleteSAML(ctx, o.samlProviderID) }})
		}
	}

	var firstErr error
	for _, job := range jobs {
		fmt.Fprintf(os.Stdout, "[mas-auth] DELETE /config/%s/%s\n", job.kind, job.idpId)
		if err := job.delete(ctx); err != nil {
			if o.ignoreMissing && masadmin.IsNotFound(err) {
				fmt.Fprintf(os.Stdout, "[mas-auth] %s/%s not found, skipping\n", job.kind, job.idpId)
				continue
			}
			fmt.Fprintf(os.Stderr, "[mas-auth] DELETE %s/%s failed: %v\n", job.kind, job.idpId, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		fmt.Fprintf(os.Stdout, "[mas-auth] %s/%s deleted\n", job.kind, job.idpId)
	}
	if firstErr != nil {
		return errors.New("one or more deletions failed; see output above")
	}
	return nil
}
