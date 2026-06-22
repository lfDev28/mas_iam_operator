package app

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	executil "github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/exec"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/ui"
)

const (
	defaultMASAuthLDAPID      = "mas-est-ldap"
	defaultMASAuthOIDCID      = "mas-est-oidc"
	defaultMASAuthSAMLID      = "mas-est-saml"
	defaultKeycloakRealm      = "maximo"
	defaultMASAuthSAMLNameID  = "email"
	defaultMASAuthSAMLSPName  = "mas-est-saml-sp"
	defaultMASAuthDisplayLDAP = "MAS EST LDAP"
	defaultMASAuthDisplayOIDC = "MAS EST OIDC"
	defaultMASAuthDisplaySAML = "MAS EST SAML"
)

type masAuthApplyOptions struct {
	namespace        string
	release          string
	realm            string
	masInstanceID    string
	masCoreNamespace string
	masAuthHost      string
	providers        []string
	ldapProviderID   string
	oidcProviderID   string
	samlProviderID   string
	wait             bool
	timeout          time.Duration

	// API-path inputs. When useCRApply is false (the default) the providers
	// are configured by PUT to the MAS Admin API at apiBaseURL using the
	// (apiTokenName, apiTokenValue) credential pair to mint a JWT via
	// /v1/authenticate. When true, fall back to the legacy IDPCfg-CR path.
	useCRApply         bool
	apiBaseURL         string
	apiTokenName       string
	apiTokenValue      string
	insecureTLS        bool
	selfRegWorkspaceID string
}

type keycloakMASClients struct {
	BaseURL          string
	OIDCClientID     string
	OIDCClientSecret string
	SAMLClientID     string
	SAMLMetadata     string
	RouteCACerts     []certificateEntry
}

type certificateEntry struct {
	Alias string `json:"alias"`
	CRT   string `json:"crt"`
}

func newMASAuthCommand() *cobra.Command {
	defaults := config.LoadInstallConfigFromEnv()
	opts := &masAuthApplyOptions{
		namespace:      defaults.Namespace,
		release:        config.DefaultIAMRelease,
		realm:          defaultKeycloakRealm,
		providers:      defaults.MASAuthProviders,
		ldapProviderID: defaultMASAuthLDAPID,
		oidcProviderID: defaultMASAuthOIDCID,
		samlProviderID: defaultMASAuthSAMLID,
		wait:           true,
		timeout:        10 * time.Minute,

		apiBaseURL:    defaults.MASBaseURL,
		apiTokenName:  defaults.MASAPITokenName,
		apiTokenValue: defaults.MASAPITokenValue,
		insecureTLS:   true,
	}

	command := &cobra.Command{
		Use:   "mas-auth",
		Short: "Configure MAS authentication providers for MAS EST IAM services",
	}
	applyCommand := &cobra.Command{
		Use:   "apply",
		Short: "Create MAS LDAP, OIDC, and SAML IDPCfg resources",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.run(cmd.Context())
		},
	}
	flags := applyCommand.Flags()
	flags.StringVar(&opts.namespace, "namespace", opts.namespace, "MAS EST namespace")
	flags.StringVar(&opts.release, "release", opts.release, "MAS EST IAM stack release name")
	flags.StringVar(&opts.realm, "realm", opts.realm, "Keycloak realm")
	flags.StringVar(&opts.masInstanceID, "mas-instance-id", opts.masInstanceID, "MAS instance ID")
	flags.StringVar(&opts.masCoreNamespace, "mas-core-namespace", opts.masCoreNamespace, "MAS core namespace")
	flags.StringVar(&opts.masAuthHost, "mas-auth-host", opts.masAuthHost, "MAS auth route host")
	flags.StringSliceVar(&opts.providers, "providers", opts.providers, "Comma-separated MAS auth providers to create: ldap,oidc,saml")
	flags.StringVar(&opts.ldapProviderID, "ldap-provider-id", opts.ldapProviderID, "MAS LDAP provider ID")
	flags.StringVar(&opts.oidcProviderID, "oidc-provider-id", opts.oidcProviderID, "MAS OIDC provider ID")
	flags.StringVar(&opts.samlProviderID, "saml-provider-id", opts.samlProviderID, "MAS SAML provider ID")
	flags.BoolVar(&opts.wait, "wait", opts.wait, "Wait for IDP Ready conditions")
	flags.DurationVar(&opts.timeout, "timeout", opts.timeout, "Time to wait for IDP readiness")
	flags.BoolVar(&opts.useCRApply, "use-cr-apply", opts.useCRApply, "Fall back to applying IDPCfg CRs directly instead of using the MAS Admin API")
	flags.StringVar(&opts.apiBaseURL, "mas-api-base-url", opts.apiBaseURL, "MAS API base URL (or SCIM URL; /scim/v2 is stripped); auto-discovered from MAS routes when omitted")
	flags.StringVar(&opts.apiTokenName, "mas-api-token-name", opts.apiTokenName, "MAS API key name (basic-auth user for /v1/authenticate); read from scim-bridge-secret when omitted")
	flags.StringVar(&opts.apiTokenValue, "mas-api-token-value", opts.apiTokenValue, "MAS API key value (basic-auth password for /v1/authenticate); read from scim-bridge-secret when omitted")
	flags.BoolVar(&opts.insecureTLS, "insecure-tls", opts.insecureTLS, "Skip TLS verification when talking to the MAS API (cluster routes commonly use cluster-local CAs)")
	flags.StringVar(&opts.selfRegWorkspaceID, "self-reg-workspace", opts.selfRegWorkspaceID, "Workspace id assigned to self-registered users in the {instance}-selfreg ConfigMap (default: workspace)")
	command.AddCommand(applyCommand)
	command.AddCommand(newMASAuthDeleteCommand())
	return command
}

func (o *masAuthApplyOptions) run(ctx context.Context) error {
	runner := executil.NewRunner()
	client := oc.NewClient(runner)
	if _, err := ensureClusterLogin(ctx, runner, client, ui.IsInteractive()); err != nil {
		return err
	}
	if err := o.defaultAndValidate(ctx, client); err != nil {
		return err
	}

	kc := keycloakMASClients{}
	if o.needsKeycloak() {
		fmt.Fprintf(os.Stdout, "[mas-auth] configuring Keycloak clients in %s/%s\n", o.namespace, o.release)
		var err error
		kc, err = o.configureKeycloak(ctx, runner, client)
		if err != nil {
			return err
		}
	}

	if o.useCRApply {
		if err := o.applyViaCR(ctx, client, kc); err != nil {
			return err
		}
	} else {
		if err := o.applyViaAdminAPI(ctx, client, kc); err != nil {
			return err
		}
	}

	if err := applyConnectionDetails(ctx, client, o.namespace, o.details(kc)); err != nil {
		return err
	}
	if err := o.applyPerProviderConnectionSecrets(ctx, client, kc); err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "MAS auth provider details")
	if o.hasProvider(config.MASAuthProviderLDAP) {
		fmt.Fprintf(os.Stdout, "  LDAP IDPCfg: %s/%s\n", o.masCoreNamespace, o.ldapIDPCfgName())
		fmt.Fprintf(os.Stdout, "  LDAP connection secret: %s/%s\n", o.namespace, ldapConnectionSecretName)
	}
	if o.hasProvider(config.MASAuthProviderOIDC) {
		fmt.Fprintf(os.Stdout, "  OIDC IDPCfg: %s/%s\n", o.masCoreNamespace, o.oidcIDPCfgName())
		fmt.Fprintf(os.Stdout, "  OIDC connection secret: %s/%s\n", o.namespace, oidcConnectionSecretName)
	}
	if o.hasProvider(config.MASAuthProviderSAML) {
		fmt.Fprintf(os.Stdout, "  SAML IDPCfg: %s/%s\n", o.masCoreNamespace, o.samlIDPCfgName())
		fmt.Fprintf(os.Stdout, "  SAML connection secret: %s/%s\n", o.namespace, samlConnectionSecretName)
	}
	fmt.Fprintf(os.Stdout, "  Details: mas-est details --namespace %s --component all\n", o.namespace)
	return nil
}

// applyPerProviderConnectionSecrets writes one Opaque Secret per configured
// provider (mas-est-ldap-connection, mas-est-oidc-connection,
// mas-est-saml-connection) into the MAS-EST namespace. Each one contains
// every connection value a downstream consumer would want — bind credentials
// for LDAP, OIDC client id/secret + endpoints, SAML entity id + IdP metadata
// XML — so a support engineer can mount or `oc get` a single secret per
// service instead of assembling values from IDPCfg + creds-secret + Keycloak.
func (o *masAuthApplyOptions) applyPerProviderConnectionSecrets(ctx context.Context, client *oc.Client, kc keycloakMASClients) error {
	if o.hasProvider(config.MASAuthProviderLDAP) {
		data, err := o.ldapConnectionSecretData(ctx, client)
		if err != nil {
			return fmt.Errorf("build LDAP connection secret payload: %w", err)
		}
		if err := applyProviderConnectionSecret(ctx, client, o.namespace, ldapConnectionSecretName, "ldap", data); err != nil {
			return err
		}
	}
	if o.hasProvider(config.MASAuthProviderOIDC) {
		if err := applyProviderConnectionSecret(ctx, client, o.namespace, oidcConnectionSecretName, "oidc", o.oidcConnectionSecretData(kc)); err != nil {
			return err
		}
	}
	if o.hasProvider(config.MASAuthProviderSAML) {
		if err := applyProviderConnectionSecret(ctx, client, o.namespace, samlConnectionSecretName, "saml", o.samlConnectionSecretData(kc)); err != nil {
			return err
		}
	}
	return nil
}

func (o *masAuthApplyOptions) ldapConnectionSecretData(ctx context.Context, client *oc.Client) (map[string]string, error) {
	bindPassword, err := ldapBindPassword(ctx, client, o.namespace, o.release)
	if err != nil {
		return nil, err
	}
	data := map[string]string{
		"url":          fmt.Sprintf("ldaps://%s-openldap.%s.svc.cluster.local:636", o.release, o.namespace),
		"baseDN":       defaultLDAPBaseDN,
		"bindDN":       fmt.Sprintf("cn=admin,%s", defaultLDAPBaseDN),
		"bindPassword": bindPassword,
		"userIdMap":    "uid",
	}
	if ca := joinCertificatePEM(o.ldapCACerts(ctx, client)); ca != "" {
		data["ca.crt"] = ca
	}
	return data, nil
}

func (o *masAuthApplyOptions) oidcConnectionSecretData(kc keycloakMASClients) map[string]string {
	return map[string]string{
		"issuerUrl":             fmt.Sprintf("%s/realms/%s", kc.BaseURL, o.realm),
		"discoveryUrl":          fmt.Sprintf("%s/realms/%s/.well-known/openid-configuration", kc.BaseURL, o.realm),
		"authorizationEndpoint": fmt.Sprintf("%s/realms/%s/protocol/openid-connect/auth", kc.BaseURL, o.realm),
		"tokenEndpoint":         fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token", kc.BaseURL, o.realm),
		"jwksEndpoint":          fmt.Sprintf("%s/realms/%s/protocol/openid-connect/certs", kc.BaseURL, o.realm),
		"clientId":              kc.OIDCClientID,
		"clientSecret":          kc.OIDCClientSecret,
		"realm":                 o.realm,
		"redirectUri":           fmt.Sprintf("https://%s/oidcclient/redirect/%s", o.masAuthHost, o.oidcProviderID),
	}
}

func (o *masAuthApplyOptions) samlConnectionSecretData(kc keycloakMASClients) map[string]string {
	entityID := samlIssuerURL(o.masAuthHost, defaultMASAuthSAMLSPName)
	data := map[string]string{
		"entityId":       entityID,
		"acsUrl":         entityID,
		"sloUrl":         fmt.Sprintf("https://%s/ibm/saml20/%s/slo", o.masAuthHost, defaultMASAuthSAMLSPName),
		"idpMetadataUrl": fmt.Sprintf("%s/realms/%s/protocol/saml/descriptor", kc.BaseURL, o.realm),
		"nameIdFormat":   defaultMASAuthSAMLNameID,
	}
	if metadata := strings.TrimSpace(kc.SAMLMetadata); metadata != "" {
		data["idpMetadata"] = metadata
	}
	return data
}

func joinCertificatePEM(certs []certificateEntry) string {
	if len(certs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(certs))
	for _, cert := range certs {
		trimmed := strings.TrimSpace(cert.CRT)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, "\n")
}

// applyViaCR is the legacy path: build IDPCfg CRs and credentials Secrets,
// `oc apply` them, then wait for the entitymgr-idpcfg controller to reconcile.
// Kept behind the --use-cr-apply flag as a fallback.
func (o *masAuthApplyOptions) applyViaCR(ctx context.Context, client *oc.Client, kc keycloakMASClients) error {
	if o.hasProvider(config.MASAuthProviderOIDC) {
		oidcSupported, err := client.IDPCfgSupportsOIDC(ctx)
		if err != nil {
			return fmt.Errorf("check MAS IDPCfg OIDC support: %w", err)
		}
		if !oidcSupported {
			return fmt.Errorf("MAS IDPCfg OIDC support was not found on this cluster; omit --providers oidc or use a MAS version whose idpcfgs.config.mas.ibm.com CRD includes spec.oidc")
		}
	}

	fmt.Fprintf(os.Stdout, "[mas-auth] applying MAS IDPCfg resources in %s\n", o.masCoreNamespace)
	manifests, err := o.masManifests(ctx, client, kc)
	if err != nil {
		return err
	}
	for _, manifest := range manifests {
		if err := applyObjectStorageManifest(ctx, client, manifest); err != nil {
			return err
		}
	}

	if o.wait {
		for _, name := range o.idpCfgNames() {
			if _, err := client.WaitForCondition(ctx, o.masCoreNamespace, "idpcfg/"+name, "Ready", o.timeout.String()); err != nil {
				return fmt.Errorf("wait for idpcfg/%s in namespace %s: %w", name, o.masCoreNamespace, err)
			}
		}
	}
	return nil
}

func (o *masAuthApplyOptions) defaultAndValidate(ctx context.Context, client *oc.Client) error {
	o.namespace = defaultString(o.namespace, config.DefaultNamespace)
	o.release = defaultString(o.release, config.DefaultIAMRelease)
	o.realm = defaultString(o.realm, defaultKeycloakRealm)
	providers, err := config.NormalizeMASAuthProviders(o.providers)
	if err != nil {
		return err
	}
	if len(providers) == 0 {
		providers = config.DefaultMASAuthProviders()
	}
	o.providers = providers
	o.ldapProviderID = defaultString(o.ldapProviderID, defaultMASAuthLDAPID)
	o.oidcProviderID = defaultString(o.oidcProviderID, defaultMASAuthOIDCID)
	o.samlProviderID = defaultString(o.samlProviderID, defaultMASAuthSAMLID)
	if o.timeout == 0 {
		o.timeout = 10 * time.Minute
	}
	o.masInstanceID = strings.TrimSpace(o.masInstanceID)
	o.masCoreNamespace = strings.TrimSpace(o.masCoreNamespace)
	if o.masCoreNamespace == "" && o.masInstanceID != "" {
		o.masCoreNamespace = "mas-" + o.masInstanceID + "-core"
	}
	if o.masInstanceID == "" && strings.HasPrefix(o.masCoreNamespace, "mas-") && strings.HasSuffix(o.masCoreNamespace, "-core") {
		o.masInstanceID = strings.TrimSuffix(strings.TrimPrefix(o.masCoreNamespace, "mas-"), "-core")
	}
	if o.masInstanceID == "" {
		return fmt.Errorf("--mas-instance-id is required")
	}
	if o.masCoreNamespace == "" {
		return fmt.Errorf("--mas-core-namespace is required")
	}
	if o.masAuthHost == "" || (!o.useCRApply && strings.TrimSpace(o.apiBaseURL) == "") {
		routes, err := client.MASRoutes(ctx)
		if err == nil {
			for _, route := range routes {
				if route.InstanceID != o.masInstanceID {
					continue
				}
				if o.masAuthHost == "" {
					o.masAuthHost = strings.Replace(route.Host, "api.", "auth.", 1)
				}
				if !o.useCRApply && strings.TrimSpace(o.apiBaseURL) == "" {
					o.apiBaseURL = "https://" + route.Host
				}
				break
			}
		}
	}
	if strings.TrimSpace(o.masAuthHost) == "" {
		return fmt.Errorf("--mas-auth-host is required")
	}
	o.masAuthHost = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(o.masAuthHost), "https://"), "http://")

	if !o.useCRApply {
		if err := o.resolveAdminAPICredentials(ctx, client); err != nil {
			return err
		}
	}
	return nil
}

// resolveAdminAPICredentials fills apiBaseURL / apiTokenName / apiTokenValue
// for the Admin API path. Anything not set via flag/env is read from the
// scim-bridge-secret in the MAS-EST namespace, which is the same place the
// SCIM bridge reads its MAS API key from.
func (o *masAuthApplyOptions) resolveAdminAPICredentials(ctx context.Context, client *oc.Client) error {
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
		return fmt.Errorf("--mas-api-base-url is required (no MAS api route discovered for instance %q)", o.masInstanceID)
	}
	if strings.TrimSpace(o.apiTokenName) == "" {
		return fmt.Errorf("--mas-api-token-name is required (set the flag, %s, or store it in secret %s/%s)", config.EnvMASAPITokenName, o.namespace, scimBridgeSecretName)
	}
	if strings.TrimSpace(o.apiTokenValue) == "" {
		return fmt.Errorf("--mas-api-token-value is required (set the flag, %s, or store it in secret %s/%s)", config.EnvMASAPITokenValue, o.namespace, scimBridgeSecretName)
	}
	return nil
}

func (o *masAuthApplyOptions) hasProvider(provider string) bool {
	for _, candidate := range o.providers {
		if candidate == provider {
			return true
		}
	}
	return false
}

func (o *masAuthApplyOptions) needsKeycloak() bool {
	return o.hasProvider(config.MASAuthProviderOIDC) || o.hasProvider(config.MASAuthProviderSAML)
}

func (o *masAuthApplyOptions) configureKeycloak(ctx context.Context, runner *executil.Runner, client *oc.Client) (keycloakMASClients, error) {
	routeHost, err := o.keycloakRouteHost(ctx, runner)
	if err != nil {
		return keycloakMASClients{}, err
	}
	service, err := o.keycloakService(ctx, runner)
	if err != nil {
		return keycloakMASClients{}, err
	}
	pod, err := o.keycloakPod(ctx, runner)
	if err != nil {
		return keycloakMASClients{}, err
	}
	admin, adminSecretName, err := o.keycloakAdminSecret(ctx, client)
	if err != nil {
		return keycloakMASClients{}, err
	}
	adminUser := firstNonEmpty(admin["username"], "admin")
	adminPassword := admin["password"]
	if adminPassword == "" {
		return keycloakMASClients{}, fmt.Errorf("Keycloak admin secret %s/%s is missing password", o.namespace, adminSecretName)
	}
	oidcSecret, err := randomSecretString(32)
	if err != nil {
		return keycloakMASClients{}, err
	}

	script := keycloakMASClientScript(map[string]string{
		"KC_ADMIN_USER":      adminUser,
		"KC_ADMIN_PASSWORD":  adminPassword,
		"KC_SERVICE":         fmt.Sprintf("http://%s:%d", service, 8080),
		"KC_REALM":           o.realm,
		"CONFIGURE_OIDC":     boolString(o.hasProvider(config.MASAuthProviderOIDC)),
		"OIDC_CLIENT_ID":     o.oidcProviderID,
		"OIDC_CLIENT_SECRET": oidcSecret,
		"OIDC_REDIRECT_URI":  fmt.Sprintf("https://%s/oidcclient/redirect/%s", o.masAuthHost, o.oidcProviderID),
		"CONFIGURE_SAML":     boolString(o.hasProvider(config.MASAuthProviderSAML)),
		// Keycloak's SAML client clientId MUST match the <saml:Issuer> that
		// MAS sends on the AuthnRequest. IBM Liberty derives that issuer as
		// https://<auth-host>/ibm/saml20/<serviceProviderName> — a URL, NOT
		// the bare SP name. Mismatch surfaces in Keycloak as
		// `client_not_found, Cannot_match_source_hash` and the user sees
		// "Invalid Request". See memory: saml-login-invalid-request.
		"SAML_CLIENT_ID":    samlIssuerURL(o.masAuthHost, defaultMASAuthSAMLSPName),
		"SAML_SP_NAME":      defaultMASAuthSAMLSPName,
		"SAML_REDIRECT_URI": fmt.Sprintf("https://%s/*", o.masAuthHost),
		// MAS Liberty's SP single-logout endpoint mirrors the ACS path. Without
		// these on the Keycloak client, SP-initiated logout dies with
		// "Can't finish SAML logout as there is no logout binding set."
		"SAML_SLO_URL": fmt.Sprintf("https://%s/ibm/saml20/%s/slo", o.masAuthHost, defaultMASAuthSAMLSPName),
	})
	if _, err := runner.InputOutput(ctx, executil.Options{
		Name: "oc",
		Args: []string{"exec", "-i", "-n", o.namespace, pod, "-c", "keycloak", "--", "bash", "-s"},
	}, strings.NewReader(script)); err != nil {
		return keycloakMASClients{}, fmt.Errorf("configure Keycloak MAS clients: %w", err)
	}

	metadata := ""
	if o.hasProvider(config.MASAuthProviderSAML) {
		metadata, err = fetchKeycloakSAMLMetadata(ctx, routeHost, o.realm)
		if err != nil {
			return keycloakMASClients{}, err
		}
	}
	certs := tlsCertificatesFromHost(ctx, routeHost, "keycloak-route")
	if len(certs) == 0 {
		certs = o.keycloakRouteCACerts(ctx, client)
	}
	return keycloakMASClients{
		BaseURL:          "https://" + routeHost,
		OIDCClientID:     o.oidcProviderID,
		OIDCClientSecret: oidcSecret,
		SAMLClientID:     samlIssuerURL(o.masAuthHost, defaultMASAuthSAMLSPName),
		SAMLMetadata:     metadata,
		RouteCACerts:     certs,
	}, nil
}

func tlsCertificatesFromHost(ctx context.Context, host, prefix string) []certificateEntry {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	rawConn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, "443"))
	if err != nil {
		return nil
	}
	defer rawConn.Close()

	conn := tls.Client(rawConn, &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         host,
	})
	if err := conn.HandshakeContext(ctx); err != nil {
		return nil
	}

	entries := []certificateEntry{}
	for i, cert := range conn.ConnectionState().PeerCertificates {
		block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
		if len(block) == 0 {
			continue
		}
		entries = append(entries, certificateEntry{
			Alias: fmt.Sprintf("%s.%d", sanitizeCertAlias(prefix), i+1),
			CRT:   strings.TrimSpace(string(block)),
		})
	}
	return entries
}

func (o *masAuthApplyOptions) keycloakAdminSecret(ctx context.Context, client *oc.Client) (map[string]string, string, error) {
	for _, secretName := range []string{o.release + "-keycloak-admin", o.release + "-bootstrap-admin"} {
		data, err := client.SecretData(ctx, o.namespace, secretName)
		if err == nil {
			return data, secretName, nil
		}
		if !oc.IsNotFound(err) {
			return nil, "", fmt.Errorf("read Keycloak admin secret %s/%s: %w", o.namespace, secretName, err)
		}
	}
	return nil, "", fmt.Errorf("Keycloak admin secret not found in namespace %s; tried %s-keycloak-admin and %s-bootstrap-admin", o.namespace, o.release, o.release)
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func keycloakMASClientScript(values map[string]string) string {
	var b strings.Builder
	b.WriteString("set -euo pipefail\n")
	for _, key := range []string{
		"KC_ADMIN_USER",
		"KC_ADMIN_PASSWORD",
		"KC_SERVICE",
		"KC_REALM",
		"CONFIGURE_OIDC",
		"OIDC_CLIENT_ID",
		"OIDC_CLIENT_SECRET",
		"OIDC_REDIRECT_URI",
		"CONFIGURE_SAML",
		"SAML_CLIENT_ID",
		"SAML_SP_NAME",
		"SAML_REDIRECT_URI",
		"SAML_SLO_URL",
	} {
		fmt.Fprintf(&b, "%s=%s\n", key, shellQuote(values[key]))
	}
	b.WriteString(`
export HOME=/tmp/mas-est-auth
mkdir -p "$HOME/.keycloak"
/opt/keycloak/bin/kcadm.sh config credentials --server "$KC_SERVICE" --realm master --user "$KC_ADMIN_USER" --password "$KC_ADMIN_PASSWORD" >/dev/null

if [[ "$CONFIGURE_OIDC" == "true" ]]; then
  cat >/tmp/mas-est-oidc-client.json <<JSON
{
  "clientId": "${OIDC_CLIENT_ID}",
  "enabled": true,
  "protocol": "openid-connect",
  "publicClient": false,
  "standardFlowEnabled": true,
  "directAccessGrantsEnabled": true,
  "serviceAccountsEnabled": false,
  "secret": "${OIDC_CLIENT_SECRET}",
  "redirectUris": ["${OIDC_REDIRECT_URI}"],
  "webOrigins": ["+"]
}
JSON
oidc_uuid=""
while IFS=, read -r client_id client_uuid _; do
  client_id="${client_id//$'\r'/}"
  client_uuid="${client_uuid//$'\r'/}"
  if [[ "$client_id" == "$OIDC_CLIENT_ID" ]]; then
    oidc_uuid="$client_uuid"
    break
  fi
done < <(/opt/keycloak/bin/kcadm.sh get clients -r "$KC_REALM" --fields clientId,id --format csv --noquotes 2>/dev/null)
if [[ -z "$oidc_uuid" ]]; then
  /opt/keycloak/bin/kcadm.sh create clients -r "$KC_REALM" -f /tmp/mas-est-oidc-client.json >/dev/null
else
  /opt/keycloak/bin/kcadm.sh update "clients/${oidc_uuid}" -r "$KC_REALM" -f /tmp/mas-est-oidc-client.json >/dev/null
fi
fi

if [[ "$CONFIGURE_SAML" == "true" ]]; then
  cat >/tmp/mas-est-saml-client.json <<JSON
{
  "clientId": "${SAML_CLIENT_ID}",
  "enabled": true,
  "protocol": "saml",
  "redirectUris": ["${SAML_REDIRECT_URI}"],
  "attributes": {
    "saml.assertion.signature": "true",
    "saml.client.signature": "false",
    "saml.force.post.binding": "true",
    "saml.server.signature": "true",
    "saml_name_id_format": "email",
    "saml_force_name_id_format": "true",
    "saml_single_logout_service_url_post": "${SAML_SLO_URL}",
    "saml_single_logout_service_url_redirect": "${SAML_SLO_URL}"
  }
}
JSON
saml_uuid=""
while IFS=, read -r client_id client_uuid _; do
  client_id="${client_id//$'\r'/}"
  client_uuid="${client_uuid//$'\r'/}"
  if [[ "$client_id" == "$SAML_CLIENT_ID" ]]; then
    saml_uuid="$client_uuid"
    break
  fi
done < <(/opt/keycloak/bin/kcadm.sh get clients -r "$KC_REALM" --fields clientId,id --format csv --noquotes 2>/dev/null)
if [[ -z "$saml_uuid" ]]; then
  /opt/keycloak/bin/kcadm.sh create clients -r "$KC_REALM" -f /tmp/mas-est-saml-client.json >/dev/null
  # re-read the uuid we just created
  while IFS=, read -r client_id client_uuid _; do
    client_id="${client_id//$'\r'/}"
    client_uuid="${client_uuid//$'\r'/}"
    if [[ "$client_id" == "$SAML_CLIENT_ID" ]]; then
      saml_uuid="$client_uuid"
      break
    fi
  done < <(/opt/keycloak/bin/kcadm.sh get clients -r "$KC_REALM" --fields clientId,id --format csv --noquotes 2>/dev/null)
else
  /opt/keycloak/bin/kcadm.sh update "clients/${saml_uuid}" -r "$KC_REALM" -f /tmp/mas-est-saml-client.json >/dev/null
fi

# Keycloak's default SAML assertion ships only the NameID + role_list — no
# user attributes. The MAS selfreg mappings (id/email/given_name/family_name)
# look up attributes by name in the assertion, so we add four
# saml-user-property mappers that emit the OIDC-style names the selfreg
# configmap expects. See memory: saml-login-invalid-request.
ensure_saml_mapper() {
  local name=$1
  local user_attr=$2
  local existing
  existing=$(/opt/keycloak/bin/kcadm.sh get "clients/${saml_uuid}/protocol-mappers/models" -r "$KC_REALM" --fields name --format csv --noquotes 2>/dev/null | tr -d '\r' | grep -Fx "$name" || true)
  if [[ -n "$existing" ]]; then
    return 0
  fi
  cat >/tmp/mas-est-saml-mapper.json <<JSON
{
  "name": "${name}",
  "protocol": "saml",
  "protocolMapper": "saml-user-property-mapper",
  "consentRequired": false,
  "config": {
    "attribute.nameformat": "Basic",
    "user.attribute": "${user_attr}",
    "attribute.name": "${name}",
    "friendly.name": "${name}"
  }
}
JSON
  /opt/keycloak/bin/kcadm.sh create "clients/${saml_uuid}/protocol-mappers/models" -r "$KC_REALM" -f /tmp/mas-est-saml-mapper.json >/dev/null
}
ensure_saml_mapper preferred_username username
ensure_saml_mapper email email
ensure_saml_mapper given_name firstName
ensure_saml_mapper family_name lastName
fi
`)
	return b.String()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// samlIssuerURL returns the <saml:Issuer> value MAS Liberty sends on
// AuthnRequests for the given SP. The Keycloak SAML client's clientId must
// be set to exactly this value.
func samlIssuerURL(masAuthHost, spName string) string {
	return fmt.Sprintf("https://%s/ibm/saml20/%s", masAuthHost, spName)
}

func (o *masAuthApplyOptions) keycloakRouteHost(ctx context.Context, runner *executil.Runner) (string, error) {
	for _, route := range []string{o.release, o.release + "-keycloak", "scim-bridge-keycloak"} {
		output, err := runner.Output(ctx, executil.Options{
			Name: "oc",
			Args: []string{"get", "route", route, "-n", o.namespace, "-o", "jsonpath={.spec.host}"},
		})
		if err == nil && strings.TrimSpace(output) != "" {
			return strings.TrimSpace(output), nil
		}
	}
	return "", fmt.Errorf("unable to find Keycloak route in namespace %s; tried %s, %s-keycloak, scim-bridge-keycloak", o.namespace, o.release, o.release)
}

func (o *masAuthApplyOptions) keycloakService(ctx context.Context, runner *executil.Runner) (string, error) {
	for _, service := range []string{o.release, o.release + "-keycloak", "scim-bridge-keycloak"} {
		output, err := runner.Output(ctx, executil.Options{
			Name: "oc",
			Args: []string{"get", "service", service, "-n", o.namespace, "-o", "jsonpath={.metadata.name}"},
		})
		if err == nil && strings.TrimSpace(output) != "" {
			return strings.TrimSpace(output), nil
		}
	}
	return "", fmt.Errorf("unable to find Keycloak service in namespace %s; tried %s, %s-keycloak, scim-bridge-keycloak", o.namespace, o.release, o.release)
}

func (o *masAuthApplyOptions) keycloakPod(ctx context.Context, runner *executil.Runner) (string, error) {
	output, err := runner.Output(ctx, executil.Options{
		Name: "oc",
		Args: []string{"get", "pod", "-n", o.namespace, "-l", "app.kubernetes.io/component=keycloak,app.kubernetes.io/instance=" + o.release, "-o", "jsonpath={.items[0].metadata.name}"},
	})
	if err != nil {
		return "", err
	}
	pod := strings.TrimSpace(output)
	if pod == "" {
		return "", fmt.Errorf("Keycloak pod not found in namespace %s for release %s", o.namespace, o.release)
	}
	return pod, nil
}

func fetchKeycloakSAMLMetadata(ctx context.Context, routeHost, realm string) (string, error) {
	url := fmt.Sprintf("https://%s/realms/%s/protocol/saml/descriptor", routeHost, realm)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // lab route certificates are often cluster-local CAs
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch Keycloak SAML metadata from %s: %w", url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("fetch Keycloak SAML metadata from %s: HTTP %d: %s", url, resp.StatusCode, string(raw))
	}
	return string(raw), nil
}

func (o *masAuthApplyOptions) keycloakRouteCACerts(ctx context.Context, client *oc.Client) []certificateEntry {
	for _, secretName := range []string{"scim-bridge-keycloak-route-cert", o.release + "-keycloak-route-cert", o.release + "-keycloak-openldap-tls"} {
		secret, err := client.SecretData(ctx, o.namespace, secretName)
		if err != nil {
			continue
		}
		for _, key := range []string{"keycloak-ca.crt", "ca.crt", "tls.crt"} {
			if certs := certificatesFromPEM(secret[key], secretName); len(certs) > 0 {
				return certs
			}
		}
	}
	return nil
}

func (o *masAuthApplyOptions) ldapCACerts(ctx context.Context, client *oc.Client) []certificateEntry {
	secret, err := client.SecretData(ctx, o.namespace, o.release+"-keycloak-openldap-tls")
	if err != nil {
		return nil
	}
	for _, key := range []string{"ca.crt", "tls.crt"} {
		if certs := certificatesFromPEM(secret[key], o.release+"-openldap"); len(certs) > 0 {
			return certs
		}
	}
	return nil
}

func certificatesFromPEM(raw, prefix string) []certificateEntry {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	re := regexp.MustCompile(`(?s)-----BEGIN CERTIFICATE-----.*?-----END CERTIFICATE-----`)
	matches := re.FindAllString(raw, -1)
	entries := make([]certificateEntry, 0, len(matches))
	for i, cert := range matches {
		entries = append(entries, certificateEntry{
			Alias: fmt.Sprintf("%s.%d", sanitizeCertAlias(prefix), i+1),
			CRT:   strings.TrimSpace(cert),
		})
	}
	return entries
}

// sanitizeCertAlias normalizes a free-form name into a string matching the
// MAS API alias regex `^[.&a-zA-Z0-9]{3,50}$` — note the absence of hyphens
// and underscores, so we collapse them (and any other non-alphanumeric runs)
// into '.'.
func sanitizeCertAlias(value string) string {
	value = strings.ToLower(value)
	re := regexp.MustCompile(`[^a-z0-9]+`)
	value = strings.Trim(re.ReplaceAllString(value, "."), ".")
	if len(value) < 3 {
		value = "cert"
	}
	if len(value) > 24 {
		value = value[:24]
	}
	return value
}

func (o *masAuthApplyOptions) masManifests(ctx context.Context, client *oc.Client, kc keycloakMASClients) ([]map[string]any, error) {
	manifests := []map[string]any{}
	if o.hasProvider(config.MASAuthProviderLDAP) {
		ldapCredSecret := o.masInstanceID + "-usersupplied-ldap-" + o.ldapProviderID + "-creds-system"
		bindPassword, err := ldapBindPassword(ctx, client, o.namespace, o.release)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests,
			secretStringDataManifest(o.masCoreNamespace, ldapCredSecret, map[string]string{
				"bindDN":       fmt.Sprintf("cn=admin,%s", defaultLDAPBaseDN),
				"bindPassword": bindPassword,
			}),
			o.ldapIDPCfgManifest(ldapCredSecret, o.ldapCACerts(ctx, client)),
		)
	}
	if o.hasProvider(config.MASAuthProviderOIDC) {
		oidcCredSecret := o.masInstanceID + "-usersupplied-oidc-" + o.oidcProviderID + "-creds-system"
		manifests = append(manifests,
			secretStringDataManifest(o.masCoreNamespace, oidcCredSecret, map[string]string{
				"clientId":     kc.OIDCClientID,
				"clientSecret": kc.OIDCClientSecret,
			}),
			o.oidcIDPCfgManifest(oidcCredSecret, kc),
		)
	}
	if o.hasProvider(config.MASAuthProviderSAML) {
		manifests = append(manifests, o.samlIDPCfgManifest(kc))
	}
	return manifests, nil
}

func ldapBindPassword(ctx context.Context, client *oc.Client, namespace, release string) (string, error) {
	secret, err := client.SecretData(ctx, namespace, release+"-openldap-admin")
	if err != nil {
		return "", fmt.Errorf("read LDAP admin secret %s/%s-openldap-admin: %w", namespace, release, err)
	}
	if secret["password"] == "" {
		return "", fmt.Errorf("LDAP admin secret %s/%s-openldap-admin is missing password", namespace, release)
	}
	return secret["password"], nil
}

func secretStringDataManifest(namespace, name string, data map[string]string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
			"labels": map[string]string{
				"app.kubernetes.io/managed-by": "mas-est-installer",
			},
		},
		"type":       "Opaque",
		"stringData": data,
	}
}

func (o *masAuthApplyOptions) baseIDPCfgManifest(name, providerID, displayName string) map[string]any {
	return map[string]any{
		"apiVersion": "config.mas.ibm.com/v1",
		"kind":       "IDPCfg",
		"metadata": map[string]any{
			"name":      name,
			"namespace": o.masCoreNamespace,
			"labels": map[string]string{
				"mas.ibm.com/configId":         providerID,
				"mas.ibm.com/configScope":      "system",
				"mas.ibm.com/instanceId":       o.masInstanceID,
				"app.kubernetes.io/managed-by": "mas-est-installer",
			},
		},
		"spec": map[string]any{
			"displayName": displayName,
			"enabled":     true,
			"visible":     true,
		},
	}
}

func (o *masAuthApplyOptions) ldapIDPCfgManifest(secretName string, certs []certificateEntry) map[string]any {
	manifest := o.baseIDPCfgManifest(o.ldapIDPCfgName(), o.ldapProviderID, defaultMASAuthDisplayLDAP)
	spec := manifest["spec"].(map[string]any)
	if len(certs) > 0 {
		spec["certificates"] = certs
	}
	spec["ldap"] = map[string]any{
		"url":    fmt.Sprintf("ldaps://%s-openldap.%s.svc.cluster.local:636", o.release, o.namespace),
		"baseDN": defaultLDAPBaseDN,
		// Plain attribute name, NOT the Liberty federated-LDAP "*:uid" syntax.
		// On MAS 9.1.18+ the customUserRegistry passes this value directly into
		// a JNDI search filter — anything other than a bare LDAP attribute
		// description throws InvalidSearchFilterException at runtime and the
		// user sees a misleading "invalid user ID or password" error. The MAS
		// Admin API docs use the same plain form (example shows "cn").
		"userIdMap": "uid",
		"credentials": map[string]string{
			"secretName": secretName,
		},
	}
	return manifest
}

func (o *masAuthApplyOptions) oidcIDPCfgManifest(secretName string, kc keycloakMASClients) map[string]any {
	manifest := o.baseIDPCfgManifest(o.oidcIDPCfgName(), o.oidcProviderID, defaultMASAuthDisplayOIDC)
	spec := manifest["spec"].(map[string]any)
	if len(kc.RouteCACerts) > 0 {
		spec["certificates"] = kc.RouteCACerts
	}
	spec["oidc"] = map[string]any{
		"discoveryEndpointUrl":              fmt.Sprintf("%s/realms/%s/.well-known/openid-configuration", kc.BaseURL, o.realm),
		"issuerIdentifier":                  fmt.Sprintf("%s/realms/%s", kc.BaseURL, o.realm),
		"authorizationEndpointUrl":          fmt.Sprintf("%s/realms/%s/protocol/openid-connect/auth", kc.BaseURL, o.realm),
		"tokenEndpointUrl":                  fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token", kc.BaseURL, o.realm),
		"jwkEndpointUrl":                    fmt.Sprintf("%s/realms/%s/protocol/openid-connect/certs", kc.BaseURL, o.realm),
		"userIdentifier":                    "preferred_username",
		"signatureAlgorithm":                "RS256",
		"tokenEndpointAuthMethod":           "basic",
		"tokenEndpointAuthSigningAlgorithm": "RS256",
		"credentials": map[string]string{
			"secretName": secretName,
		},
	}
	return manifest
}

func (o *masAuthApplyOptions) samlIDPCfgManifest(kc keycloakMASClients) map[string]any {
	manifest := o.baseIDPCfgManifest(o.samlIDPCfgName(), o.samlProviderID, defaultMASAuthDisplaySAML)
	spec := manifest["spec"].(map[string]any)
	spec["saml"] = map[string]any{
		"idp": map[string]string{
			"metadata": base64.StdEncoding.EncodeToString([]byte(kc.SAMLMetadata)),
		},
		"sp": map[string]any{
			"issuer":              o.samlProviderID,
			"serviceProviderName": defaultMASAuthSAMLSPName,
			"nameIDFormat":        defaultMASAuthSAMLNameID,
			"spInitiatedLogout":   true,
		},
	}
	return manifest
}

func (o *masAuthApplyOptions) details(kc keycloakMASClients) map[string]string {
	details := map[string]string{}
	if o.hasProvider(config.MASAuthProviderLDAP) {
		details["ldap.displayName"] = defaultMASAuthDisplayLDAP
		details["ldap.providerId"] = o.ldapProviderID
		details["ldap.idpCfg"] = fmt.Sprintf("%s/%s", o.masCoreNamespace, o.ldapIDPCfgName())
		details["ldap.url"] = fmt.Sprintf("ldaps://%s-openldap.%s.svc.cluster.local:636", o.release, o.namespace)
		details["ldap.baseDN"] = defaultLDAPBaseDN
		details["ldap.bindDN"] = fmt.Sprintf("cn=admin,%s", defaultLDAPBaseDN)
		details["ldap.bindSecret"] = fmt.Sprintf("%s/%s-openldap-admin key password", o.namespace, o.release)
		details["ldap.userIdMap"] = "uid"
	}
	if o.hasProvider(config.MASAuthProviderOIDC) {
		details["oidc.displayName"] = defaultMASAuthDisplayOIDC
		details["oidc.providerId"] = o.oidcProviderID
		details["oidc.idpCfg"] = fmt.Sprintf("%s/%s", o.masCoreNamespace, o.oidcIDPCfgName())
		details["oidc.clientId"] = kc.OIDCClientID
		details["oidc.clientSecretRef"] = fmt.Sprintf("%s/%s-usersupplied-oidc-%s-creds-system key clientSecret", o.masCoreNamespace, o.masInstanceID, o.oidcProviderID)
		details["oidc.discoveryURL"] = fmt.Sprintf("%s/realms/%s/.well-known/openid-configuration", kc.BaseURL, o.realm)
		details["oidc.redirectURI"] = fmt.Sprintf("https://%s/oidcclient/redirect/%s", o.masAuthHost, o.oidcProviderID)
	}
	if o.hasProvider(config.MASAuthProviderSAML) {
		details["saml.displayName"] = defaultMASAuthDisplaySAML
		details["saml.providerId"] = o.samlProviderID
		details["saml.idpCfg"] = fmt.Sprintf("%s/%s", o.masCoreNamespace, o.samlIDPCfgName())
		details["saml.keycloakMetadata"] = fmt.Sprintf("%s/realms/%s/protocol/saml/descriptor", kc.BaseURL, o.realm)
		details["saml.spIssuer"] = o.samlProviderID
		details["saml.nameIDFormat"] = defaultMASAuthSAMLNameID
	}
	return details
}

func (o *masAuthApplyOptions) ldapIDPCfgName() string {
	return fmt.Sprintf("%s-ldap-%s-system", o.masInstanceID, o.ldapProviderID)
}

func (o *masAuthApplyOptions) oidcIDPCfgName() string {
	return fmt.Sprintf("%s-oidc-%s-system", o.masInstanceID, o.oidcProviderID)
}

func (o *masAuthApplyOptions) samlIDPCfgName() string {
	return fmt.Sprintf("%s-saml-%s-system", o.masInstanceID, o.samlProviderID)
}

func (o *masAuthApplyOptions) idpCfgNames() []string {
	names := []string{}
	if o.hasProvider(config.MASAuthProviderLDAP) {
		names = append(names, o.ldapIDPCfgName())
	}
	if o.hasProvider(config.MASAuthProviderOIDC) {
		names = append(names, o.oidcIDPCfgName())
	}
	if o.hasProvider(config.MASAuthProviderSAML) {
		names = append(names, o.samlIDPCfgName())
	}
	return names
}
