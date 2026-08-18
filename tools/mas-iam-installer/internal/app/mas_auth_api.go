package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	executil "github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/exec"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/masadmin"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
)

// ensureIDPCfgMemoryLimit patches the Suite CR's spec.podTemplates so the
// {instance}-entitymgr-idpcfg deployment runs with at least the requested
// memory limit. The Suite operator reconciles podTemplates into the
// deployment durably — direct deployment patches get reverted by both the
// Suite operator AND the ibm-mas-operator within a couple of minutes, so
// the Suite CR is the only durable knob. Implemented per the MAS docs
// "Customizing workload scale" → "Supported pods". See memory:
// mas-entitymgr-idpcfg-oom.
//
// Idempotent: if a podTemplate for entitymgr-idpcfg already exists, the
// resources block is replaced. Other podTemplates in the array are left
// untouched.
func ensureIDPCfgMemoryLimit(ctx context.Context, runner *executil.Runner, masCoreNamespace, masInstanceID, memoryLimit string) error {
	memoryLimit = strings.TrimSpace(memoryLimit)
	if memoryLimit == "" || strings.EqualFold(memoryLimit, "off") {
		return nil
	}

	// Read the existing podTemplates array. jsonpath returns "" for absent,
	// "[]" for empty, or the JSON array text.
	current, err := runner.Output(ctx, executil.Options{
		Name: "oc",
		Args: []string{"get", "suite", masInstanceID, "-n", masCoreNamespace, "-o", "jsonpath={.spec.podTemplates}"},
	})
	if err != nil {
		return fmt.Errorf("read Suite %s/%s podTemplates: %w", masCoreNamespace, masInstanceID, err)
	}

	existing := []map[string]any{}
	if trimmed := strings.TrimSpace(current); trimmed != "" && trimmed != "[]" && trimmed != "null" {
		if err := json.Unmarshal([]byte(trimmed), &existing); err != nil {
			return fmt.Errorf("decode Suite podTemplates: %w", err)
		}
	}

	desired := idpcfgPodTemplate(memoryLimit)
	updated := false
	for i, tmpl := range existing {
		if name, _ := tmpl["name"].(string); name == "entitymgr-idpcfg" {
			existing[i] = desired
			updated = true
			break
		}
	}
	if !updated {
		existing = append(existing, desired)
	}

	raw, err := json.Marshal(existing)
	if err != nil {
		return err
	}
	patch := fmt.Sprintf(`[{"op":"replace","path":"/spec/podTemplates","value":%s}]`, raw)
	if _, err := runner.Output(ctx, executil.Options{
		Name: "oc",
		Args: []string{"patch", "suite", masInstanceID, "-n", masCoreNamespace, "--type=json", "-p", patch},
	}); err != nil {
		return fmt.Errorf("patch Suite podTemplates: %w", err)
	}
	fmt.Fprintf(os.Stdout, "[mas-auth] ensured entitymgr-idpcfg memory limit = %s via Suite CR podTemplates\n", memoryLimit)
	return nil
}

// idpcfgPodTemplate returns the podTemplates[] entry for entitymgr-idpcfg.
// Format per MAS docs "Customizing workload scale" → "Supported pods":
// container name is "manager", default request 64Mi, default limit 512Mi
// (which is what we're overriding).
func idpcfgPodTemplate(memoryLimit string) map[string]any {
	return map[string]any{
		"name": "entitymgr-idpcfg",
		"containers": []map[string]any{
			{
				"name": "manager",
				"resources": map[string]any{
					"requests": map[string]string{
						"cpu":    "0.01",
						"memory": "64Mi",
					},
					"limits": map[string]string{
						"cpu":    "0.2",
						"memory": memoryLimit,
					},
				},
			},
		},
	}
}

// providerNeedsTypeSuffix returns true when the IDPCfg idpId is the MAS
// reserved word "default". MAS Liberty / coreapi then derive both the OIDC
// redirect_uri path and the selfreg ConfigMap lookup key with a "-{type}"
// suffix (e.g. default-oidc), so we need to register matching keys on the
// Keycloak client and in the selfreg ConfigMap. Custom idpIds don't trigger
// the suffix — they're used verbatim. See memory: mas-default-idpid-reserved-word.
func providerNeedsTypeSuffix(idpId string) bool {
	return strings.TrimSpace(idpId) == reservedIDPID
}

// providerKeyWithSuffix returns the canonical per-provider key MAS uses for
// state lookups (selfreg ConfigMap, Liberty client id). For reserved-word
// idpIds the type suffix is appended; for custom idpIds the idpId is returned
// verbatim. This MUST match how MAS coreapi keys its own internal lookups —
// confirmed empirically with AIUOM0013E miss on `default` vs hit on
// `default-oidc`.
func providerKeyWithSuffix(idpId, providerType string) string {
	if providerNeedsTypeSuffix(idpId) {
		return idpId + "-" + providerType
	}
	return idpId
}

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
			// MAS 9.0 has no /config/oidc endpoint at all: the PUT 404s with
			// AIUCO1022E. Preflight catches this before install; this is the
			// belt-and-braces guard for direct mas-auth runs.
			if isOIDCAdminAPIMissing(err) {
				return fmt.Errorf("setOIDCConfig %s: %w; MAS does not expose the OIDC external-IdP API (requires MAS 9.1+); re-run with --mas-auth-providers ldap,saml", o.oidcProviderID, err)
			}
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
	// the self-reg path 409s on them. Inject the IDP identity linkage for
	// any SCIM-managed user that's missing it. OIDC is preferred when both
	// are configured; SAML is the fallback so SCIM users can still log in on
	// SAML-only installs. LDAP alone gets no linking — SCIM users don't
	// exist in OpenLDAP, so there's nothing for them to log in as.
	// Best-effort — log and continue on failure so we don't block the
	// install.
	linkType := ""
	switch {
	case o.hasProvider(config.MASAuthProviderOIDC):
		linkType = "oidc"
	case o.hasProvider(config.MASAuthProviderSAML):
		linkType = "saml"
	}
	if linkType != "" {
		if err := o.linkSCIMUsersToIDP(ctx, linkType); err != nil {
			fmt.Fprintf(os.Stderr, "[mas-auth] WARN: link-scim-users-oidc step failed (non-fatal): %v\n", err)
		}
	}
	return nil
}

// isOIDCAdminAPIMissing reports whether err is the MAS 9.0 signature for a
// missing OIDC external-IdP Admin API: a 404 from /config/oidc/{id}, whose
// body carries exception id AIUCO1022E ("The requested URL could not be
// found"). masadmin surfaces both — the status via *masadmin.APIError and the
// body via the formatted error string.
func isOIDCAdminAPIMissing(err error) bool {
	if masadmin.IsNotFound(err) {
		return true
	}
	return err != nil && strings.Contains(err.Error(), "AIUCO1022E")
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
		data[providerKeyWithSuffix(o.oidcProviderID, "oidc")] = o.selfRegConfigYAML(oidcSelfRegMappings)
	}
	if o.hasProvider(config.MASAuthProviderSAML) {
		// SAML uses the same OIDC-style mappings because the Keycloak SAML
		// client emits saml-user-property mappers with OIDC-style attribute
		// names (preferred_username, email, given_name, family_name). See
		// the SAML quirks documented in [[saml-login-invalid-request]].
		data[providerKeyWithSuffix(o.samlProviderID, "saml")] = o.selfRegConfigYAML(oidcSelfRegMappings)
	}
	if o.hasProvider(config.MASAuthProviderLDAP) {
		// LDAP login goes through MAS's customUserRegistry Liberty feature
		// (NOT a JIT IDP flow), but MAS still consults the {instance}-selfreg
		// ConfigMap when a successfully-bound LDAP user has no MongoDB record
		// — without an mas-est-ldap entry, login fails with CWIML4537E
		// "principal not found in the back-end repository". Mappings use
		// LDAP attribute names (uid/mail/cn/givenName/sn) not OIDC claims.
		data[providerKeyWithSuffix(o.ldapProviderID, "ldap")] = o.selfRegConfigYAML(ldapSelfRegMappings)
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
	if err := applyManifest(ctx, client, manifest); err != nil {
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

// linkSCIMUsersToIDP shells out to scripts/link-scim-users-oidc.sh. We use a
// shell helper because the linkage requires direct MongoDB writes against the
// MAS Mongo replica set — there is no SCIM/Admin API that exposes the
// identities field. The script is idempotent (only touches users missing the
// configured identity).
//
// The --idp arg must match the identity-key MAS Manage's MEA expects.
// For the reserved-word "default" idpId, MAS internally registers the IDP
// as "default-{type}" (same suffix applied to selfreg and redirect_uri),
// so the SCIM-user identities map must key off "default-oidc"/"default-saml"
// too — otherwise Manage workspace sync fails with system#idpnotfound at
// MASUserIDP.appValidate. See memory: scim-bridge-manage-sync-idpnotfound.
func (o *masAuthApplyOptions) linkSCIMUsersToIDP(ctx context.Context, providerType string) error {
	scriptPath, err := repoScriptPath("link-scim-users-oidc.sh")
	if err != nil {
		return err
	}
	runner := executil.NewRunner()
	out, err := runner.Output(ctx, executil.Options{
		Name: scriptPath,
		Args: o.linkSCIMUsersArgs(providerType),
	})
	if out != "" {
		fmt.Fprint(os.Stdout, out)
	}
	return err
}

// linkSCIMUsersArgs builds the argv for link-scim-users-oidc.sh. Extracted
// so the IDPCfg-rename / reserved-word translation can be unit-tested without
// shelling out.
func (o *masAuthApplyOptions) linkSCIMUsersArgs(providerType string) []string {
	providerID := o.oidcProviderID
	if providerType == "saml" {
		providerID = o.samlProviderID
	}
	return []string{
		"--instance", o.masInstanceID,
		"--idp", providerKeyWithSuffix(providerID, providerType),
		"--idp-type", providerType,
	}
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
		UserIDMap:    defaultMASAuthLDAPUserIDMap,
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
		if err := sleepCtx(ctx, pollInterval); err != nil {
			return err
		}
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

