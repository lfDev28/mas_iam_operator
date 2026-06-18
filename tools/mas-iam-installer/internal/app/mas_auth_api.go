package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	executil "github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/exec"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/masadmin"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
)

const (
	pollInterval = 5 * time.Second
)

// applyViaAdminAPI is the default mas-auth path. It PUTs each requested
// provider's configuration to the MAS Admin API sequentially (LDAP → OIDC →
// SAML SP → SAML IdP metadata), optionally polling each one to Ready before
// moving to the next.
func (o *masAuthApplyOptions) applyViaAdminAPI(ctx context.Context, client *oc.Client, kc keycloakMASClients) error {
	mc, err := o.newMASAdminClient()
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "[mas-auth] using MAS Admin API at %s\n", mc.BaseURL())

	// Ensure the per-IDP self-registration ConfigMap exists in the MAS core
	// namespace before we touch any provider. Self-reg covers the case where a
	// user authenticates via OIDC/SAML for the first time and does NOT yet
	// exist in MAS — MAS auto-creates the user record.
	if err := o.applySelfRegConfigMap(ctx, client); err != nil {
		return err
	}

	if o.hasProvider(config.MASAuthProviderLDAP) {
		fmt.Fprintf(os.Stdout, "[mas-auth] PUT /config/ldap/%s\n", o.ldapProviderID)
		req, err := o.buildLDAPRequest(ctx, client)
		if err != nil {
			return err
		}
		resp, err := mc.SetLDAP(ctx, o.ldapProviderID, req)
		if err != nil {
			return fmt.Errorf("setLDAPConfig %s: %w", o.ldapProviderID, err)
		}
		if err := o.maybeWait(ctx, mc, "ldap", o.ldapProviderID, resp); err != nil {
			return err
		}
	}

	if o.hasProvider(config.MASAuthProviderOIDC) {
		fmt.Fprintf(os.Stdout, "[mas-auth] PUT /config/oidc/%s\n", o.oidcProviderID)
		req := o.buildOIDCRequest(kc)
		resp, err := mc.SetOIDC(ctx, o.oidcProviderID, req)
		if err != nil {
			return fmt.Errorf("setOIDCConfig %s: %w", o.oidcProviderID, err)
		}
		if err := o.maybeWait(ctx, mc, "oidc", o.oidcProviderID, resp); err != nil {
			return err
		}
	}

	if o.hasProvider(config.MASAuthProviderSAML) {
		fmt.Fprintf(os.Stdout, "[mas-auth] PUT /config/saml/%s\n", o.samlProviderID)
		spReq := o.buildSAMLSPRequest()
		resp, err := mc.SetSAMLSP(ctx, o.samlProviderID, spReq)
		if err != nil {
			return fmt.Errorf("setSAMLConfig %s: %w", o.samlProviderID, err)
		}
		if kc.SAMLMetadata != "" {
			fmt.Fprintf(os.Stdout, "[mas-auth] PUT /config/saml/%s/metadata\n", o.samlProviderID)
			if _, err := mc.SetSAMLIDPMetadata(ctx, o.samlProviderID, []byte(kc.SAMLMetadata)); err != nil {
				return fmt.Errorf("setSAMLIDPConfig %s: %w", o.samlProviderID, err)
			}
		}
		if err := o.maybeWait(ctx, mc, "saml", o.samlProviderID, resp); err != nil {
			return err
		}
	}

	// Cover the other half of the chicken-and-egg with SCIM bridge: users
	// the bridge already created in MAS Core only have `_local` identity, so
	// the self-reg path 409s on them. Inject the OIDC identity linkage for
	// any SCIM-managed user that's missing it. Best-effort — log and
	// continue on failure so we don't block the install.
	if o.hasProvider(config.MASAuthProviderOIDC) {
		if err := o.linkSCIMUsersToOIDC(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "[mas-auth] WARN: link-scim-users-oidc step failed (non-fatal): %v\n", err)
		}
	}
	return nil
}

// applySelfRegConfigMap creates/updates the {instance}-selfreg ConfigMap in
// the MAS core namespace. MAS reads this configmap (key per IDP id) to decide
// whether self-registration is allowed for that IDP, how to map JWT/SAML
// claims to user fields, and which workspace/application entitlement the new
// user lands in. Without this, MAS rejects unknown OIDC/SAML users with
// AIUOM0013E even though the auth itself succeeded.
//
// Shape per MAS docs (Adding fields to the self-registration form, 2026-01-29
// and the OIDC/SAML JIT path validated 2026-06-16):
//
//	{idpId}: |
//	  enabled: true
//	  autoApproval: true
//	  appEntitlement: PREMIUM
//	  workspaces:
//	    - id: <workspace-id>
//	      applications: [manage]
//	  mappings:
//	    id: preferred_username
//	    ...
func (o *masAuthApplyOptions) applySelfRegConfigMap(ctx context.Context, client *oc.Client) error {
	data := map[string]string{}
	if o.hasProvider(config.MASAuthProviderOIDC) {
		data[o.oidcProviderID] = o.selfRegConfigYAML(oidcSelfRegMappings)
	}
	if o.hasProvider(config.MASAuthProviderSAML) {
		// SAML uses the same OIDC-style mappings because the Keycloak SAML
		// client emits saml-user-property mappers with OIDC-style attribute
		// names (preferred_username, email, given_name, family_name). See
		// the SAML quirks documented in [[saml-login-invalid-request]].
		data[o.samlProviderID] = o.selfRegConfigYAML(oidcSelfRegMappings)
	}
	if o.hasProvider(config.MASAuthProviderLDAP) {
		// LDAP login goes through MAS's customUserRegistry Liberty feature
		// (NOT a JIT IDP flow), but MAS still consults the {instance}-selfreg
		// ConfigMap when a successfully-bound LDAP user has no MongoDB record
		// — without an mas-est-ldap entry, login fails with CWIML4537E
		// "principal not found in the back-end repository". Mappings use
		// LDAP attribute names (uid/mail/cn/givenName/sn) not OIDC claims.
		data[o.ldapProviderID] = o.selfRegConfigYAML(ldapSelfRegMappings)
	}
	if len(data) == 0 {
		return nil
	}

	name := o.masInstanceID + "-selfreg"
	manifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      name,
			"namespace": o.masCoreNamespace,
			"labels": map[string]string{
				"app.kubernetes.io/managed-by": "mas-est-installer",
				"mas.ibm.com/instanceId":       o.masInstanceID,
			},
		},
		"data": data,
	}

	fmt.Fprintf(os.Stdout, "[mas-auth] applying self-reg ConfigMap %s/%s\n", o.masCoreNamespace, name)
	if err := applyObjectStorageManifest(ctx, client, manifest); err != nil {
		return fmt.Errorf("apply self-reg configmap: %w", err)
	}
	return nil
}

// oidcSelfRegMappings — OIDC standard claims. Reused for SAML because the
// Keycloak SAML client is configured with saml-user-property mappers that
// emit assertion attributes under these same names.
const oidcSelfRegMappings = `mappings:
  id: preferred_username
  email: email
  displayName: name
  givenName: given_name
  familyName: family_name
  title: title`

// ldapSelfRegMappings — LDAP attribute names. The customUserRegistry passes
// these through verbatim, so the selfreg mapping has to use LDAP attribute
// names (uid/mail/cn/...), NOT the OIDC claim names.
const ldapSelfRegMappings = `mappings:
  id: uid
  email: mail
  displayName: cn
  givenName: givenName
  familyName: sn
  title: title`

// selfRegConfigYAML renders the per-IDP configmap entry. workspaceID defaults
// to "workspace" (the {instance}-workspace ManageWorkspace CR's id suffix).
// The mappings argument is the YAML "mappings:" block (caller chooses OIDC
// claim names vs LDAP attribute names depending on the provider type).
func (o *masAuthApplyOptions) selfRegConfigYAML(mappings string) string {
	workspaceID := o.selfRegWorkspaceID
	if workspaceID == "" {
		workspaceID = "workspace"
	}
	return fmt.Sprintf(`enabled: true
autoApproval: true
appEntitlement: PREMIUM
workspaces:
  - id: %s
    applications:
      - manage
%s
`, workspaceID, mappings)
}

// linkSCIMUsersToOIDC shells out to scripts/link-scim-users-oidc.sh. We use a
// shell helper because the linkage requires direct MongoDB writes against the
// MAS Mongo replica set — there is no SCIM/Admin API that exposes the
// identities field. The script is idempotent (only touches users missing the
// configured identity).
func (o *masAuthApplyOptions) linkSCIMUsersToOIDC(ctx context.Context) error {
	scriptPath, err := repoScriptPath("link-scim-users-oidc.sh")
	if err != nil {
		return err
	}
	runner := executil.NewRunner()
	out, err := runner.Output(ctx, executil.Options{
		Name: scriptPath,
		Args: []string{
			"--instance", o.masInstanceID,
			"--oidc-idp", o.oidcProviderID,
		},
	})
	if out != "" {
		fmt.Fprint(os.Stdout, out)
	}
	return err
}

// repoScriptPath locates a script under <repo>/scripts. Order of resolution
// matches the rest of the installer: explicit MAS_EST_REPO_ROOT env > the
// installed bootstrap layout > a sibling scripts/ directory next to the
// running binary.
func repoScriptPath(name string) (string, error) {
	if root := os.Getenv("MAS_EST_REPO_ROOT"); root != "" {
		p := filepath.Join(root, "scripts", name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if root := os.Getenv("MAS_IAM_REPO_ROOT"); root != "" {
		p := filepath.Join(root, "scripts", name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	// Best-effort: try relative to working directory (developer flow).
	for _, candidate := range []string{
		filepath.Join("scripts", name),
		filepath.Join("..", "..", "scripts", name),
		filepath.Join("..", "..", "..", "scripts", name),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("script %s not found; set MAS_EST_REPO_ROOT", name)
}

func (o *masAuthApplyOptions) newMASAdminClient() (*masadmin.Client, error) {
	opts := []masadmin.Option{}
	if o.insecureTLS {
		opts = append(opts, masadmin.WithInsecureTLS())
	}
	return masadmin.NewClient(o.apiBaseURL, o.apiTokenName, o.apiTokenValue, opts...)
}

func (o *masAuthApplyOptions) buildLDAPRequest(ctx context.Context, client *oc.Client) (masadmin.LDAPConfigRequest, error) {
	bindPassword, err := ldapBindPassword(ctx, client, o.namespace, o.release)
	if err != nil {
		return masadmin.LDAPConfigRequest{}, err
	}
	return masadmin.LDAPConfigRequest{
		DisplayName:  defaultMASAuthDisplayLDAP,
		URL:          fmt.Sprintf("ldaps://%s-openldap.%s.svc.cluster.local:636", o.release, o.namespace),
		BaseDN:       defaultLDAPBaseDN,
		BindDN:       fmt.Sprintf("cn=admin,%s", defaultLDAPBaseDN),
		BindPassword: bindPassword,
		UserIDMap:    "*:uid",
		Certificates: toMASAdminCerts(o.ldapCACerts(ctx, client)),
	}, nil
}

func (o *masAuthApplyOptions) buildOIDCRequest(kc keycloakMASClients) masadmin.OIDCConfigRequest {
	return masadmin.OIDCConfigRequest{
		DisplayName:                       defaultMASAuthDisplayOIDC,
		ClientID:                          kc.OIDCClientID,
		ClientSecret:                      kc.OIDCClientSecret,
		DiscoveryEndpointURL:              fmt.Sprintf("%s/realms/%s/.well-known/openid-configuration", kc.BaseURL, o.realm),
		IssuerIdentifier:                  fmt.Sprintf("%s/realms/%s", kc.BaseURL, o.realm),
		AuthorizationEndpointURL:          fmt.Sprintf("%s/realms/%s/protocol/openid-connect/auth", kc.BaseURL, o.realm),
		TokenEndpointURL:                  fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token", kc.BaseURL, o.realm),
		JWKEndpointURL:                    fmt.Sprintf("%s/realms/%s/protocol/openid-connect/certs", kc.BaseURL, o.realm),
		UserIdentifier:                    "preferred_username",
		SignatureAlgorithm:                "RS256",
		TokenEndpointAuthMethod:           "basic",
		TokenEndpointAuthSigningAlgorithm: "RS256",
		Certificates:                      toMASAdminCerts(kc.RouteCACerts),
	}
}

func (o *masAuthApplyOptions) buildSAMLSPRequest() masadmin.SAMLSPConfigRequest {
	spInit := true
	return masadmin.SAMLSPConfigRequest{
		DisplayName:         defaultMASAuthDisplaySAML,
		Issuer:              o.samlProviderID,
		ServiceProviderName: defaultMASAuthSAMLSPName,
		NameIDFormat:        defaultMASAuthSAMLNameID,
		SPInitiatedLogout:   &spInit,
	}
}

// maybeWait polls the given provider until Ready or until o.timeout elapses,
// when o.wait is true. If the initial PUT response already reports Ready,
// no polling is performed.
func (o *masAuthApplyOptions) maybeWait(ctx context.Context, mc *masadmin.Client, kind, idpId string, initial *masadmin.IDPConfigResponse) error {
	if !o.wait {
		return nil
	}
	if initial != nil && initial.Ready() {
		fmt.Fprintf(os.Stdout, "[mas-auth] %s/%s Ready\n", kind, idpId)
		return nil
	}
	deadline := time.Now().Add(o.timeout)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var (
			resp *masadmin.IDPConfigResponse
			err  error
		)
		switch kind {
		case "ldap":
			resp, err = mc.GetLDAP(ctx, idpId)
		case "oidc":
			resp, err = mc.GetOIDC(ctx, idpId)
		case "saml":
			resp, err = mc.GetSAML(ctx, idpId)
		default:
			return fmt.Errorf("maybeWait: unknown kind %q", kind)
		}
		if err != nil && !masadmin.IsNotFound(err) {
			return fmt.Errorf("poll %s/%s: %w", kind, idpId, err)
		}
		if resp != nil && resp.Ready() {
			fmt.Fprintf(os.Stdout, "[mas-auth] %s/%s Ready\n", kind, idpId)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting %s for %s/%s to become Ready: %s", o.timeout, kind, idpId, conditionSummary(resp))
		}
		time.Sleep(pollInterval)
	}
}

func conditionSummary(resp *masadmin.IDPConfigResponse) string {
	if resp == nil {
		return "no conditions reported yet"
	}
	if len(resp.Status.Conditions) == 0 {
		return "no conditions reported yet"
	}
	out := "last conditions:"
	for _, c := range resp.Status.Conditions {
		out += fmt.Sprintf(" %s=%s(%s)", c.Type, c.Status, c.Reason)
	}
	return out
}

// toMASAdminCerts converts internal certificate entries to the wire type.
// Returns a non-nil empty slice when there are no entries — the MAS API
// requires `certificates` to be present in the JSON body (even if empty);
// a nil slice would marshal to `null` and fail the schema validation.
func toMASAdminCerts(entries []certificateEntry) []masadmin.Certificate {
	out := make([]masadmin.Certificate, 0, len(entries))
	for _, entry := range entries {
		out = append(out, masadmin.Certificate{Alias: entry.Alias, CRT: entry.CRT})
	}
	return out
}

