package app

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/masadmin"
)

func TestMASAuthIDPCfgNamesUseStableProviderIDs(t *testing.T) {
	opts := &masAuthApplyOptions{
		masInstanceID:  "lfmas",
		ldapProviderID: defaultMASAuthLDAPID,
		oidcProviderID: defaultMASAuthOIDCID,
		samlProviderID: defaultMASAuthSAMLID,
	}

	if got := opts.ldapIDPCfgName(); got != "lfmas-ldap-mas-est-ldap-system" {
		t.Fatalf("ldapIDPCfgName() = %q", got)
	}
	if got := opts.oidcIDPCfgName(); got != "lfmas-oidc-mas-est-oidc-system" {
		t.Fatalf("oidcIDPCfgName() = %q", got)
	}
	if got := opts.samlIDPCfgName(); got != "lfmas-saml-mas-est-saml-system" {
		t.Fatalf("samlIDPCfgName() = %q", got)
	}
}

func TestMASAuthLDAPManifest(t *testing.T) {
	opts := &masAuthApplyOptions{
		namespace:        "mas-est",
		release:          "mas-est-iam",
		masInstanceID:    "lfmas",
		masCoreNamespace: "mas-lfmas-core",
		ldapProviderID:   defaultMASAuthLDAPID,
	}

	manifest := opts.ldapIDPCfgManifest("ldap-creds", nil)
	metadata := manifest["metadata"].(map[string]any)
	labels := metadata["labels"].(map[string]string)
	if labels["mas.ibm.com/configId"] != defaultMASAuthLDAPID {
		t.Fatalf("labels = %v", labels)
	}
	spec := manifest["spec"].(map[string]any)
	ldap := spec["ldap"].(map[string]any)
	if ldap["url"] != "ldaps://mas-est-iam-openldap.mas-est.svc.cluster.local:636" {
		t.Fatalf("ldap url = %q", ldap["url"])
	}
	// Plain attribute name — Liberty's "*:uid" syntax breaks MAS 9.1.18's
	// customUserRegistry which passes the value directly into a JNDI filter.
	if ldap["userIdMap"] != "uid" {
		t.Fatalf("userIdMap = %q", ldap["userIdMap"])
	}
}

func TestMASAuthOIDCManifestUsesWellKnownAndOpenIDConnectHyphens(t *testing.T) {
	opts := &masAuthApplyOptions{
		realm:            "maximo",
		masInstanceID:    "lfmas",
		masCoreNamespace: "mas-lfmas-core",
		oidcProviderID:   defaultMASAuthOIDCID,
	}
	kc := keycloakMASClients{
		BaseURL: "https://keycloak.apps.example.com",
	}

	manifest := opts.oidcIDPCfgManifest("oidc-creds", kc)
	spec := manifest["spec"].(map[string]any)
	oidc := spec["oidc"].(map[string]any)
	if !strings.Contains(oidc["discoveryEndpointUrl"].(string), ".well-known/openid-configuration") {
		t.Fatalf("discoveryEndpointUrl = %q", oidc["discoveryEndpointUrl"])
	}
	if !strings.Contains(oidc["jwkEndpointUrl"].(string), "openid-connect/certs") {
		t.Fatalf("jwkEndpointUrl = %q", oidc["jwkEndpointUrl"])
	}
	if oidc["tokenEndpointAuthMethod"] != "basic" {
		t.Fatalf("tokenEndpointAuthMethod = %q", oidc["tokenEndpointAuthMethod"])
	}
	if oidc["tokenEndpointAuthSigningAlgorithm"] != "RS256" {
		t.Fatalf("tokenEndpointAuthSigningAlgorithm = %q", oidc["tokenEndpointAuthSigningAlgorithm"])
	}
}

func TestMASAuthSAMLManifestEncodesMetadata(t *testing.T) {
	opts := &masAuthApplyOptions{
		masInstanceID:    "lfmas",
		masCoreNamespace: "mas-lfmas-core",
		samlProviderID:   defaultMASAuthSAMLID,
	}
	kc := keycloakMASClients{
		SAMLMetadata: "<EntityDescriptor />",
	}

	manifest := opts.samlIDPCfgManifest(kc)
	spec := manifest["spec"].(map[string]any)
	saml := spec["saml"].(map[string]any)
	idp := saml["idp"].(map[string]string)
	decoded, err := base64.StdEncoding.DecodeString(idp["metadata"])
	if err != nil {
		t.Fatalf("metadata base64 decode error = %v", err)
	}
	if string(decoded) != kc.SAMLMetadata {
		t.Fatalf("metadata = %q", decoded)
	}
	sp := saml["sp"].(map[string]any)
	if sp["issuer"] != defaultMASAuthSAMLID {
		t.Fatalf("issuer = %q", sp["issuer"])
	}
}

func TestBuildOIDCRequestPopulatesAllKeycloakEndpoints(t *testing.T) {
	opts := &masAuthApplyOptions{
		realm:          "maximo",
		oidcProviderID: defaultMASAuthOIDCID,
	}
	kc := keycloakMASClients{
		BaseURL:          "https://keycloak.apps.example.com",
		OIDCClientID:     "mas-est-oidc",
		OIDCClientSecret: "shhh",
		RouteCACerts: []certificateEntry{
			{Alias: "keycloak-route-1", CRT: "-----BEGIN CERTIFICATE-----\nAAA\n-----END CERTIFICATE-----"},
		},
	}

	req := opts.buildOIDCRequest(kc)
	if req.ClientID != "mas-est-oidc" || req.ClientSecret != "shhh" {
		t.Fatalf("client credentials not propagated: %+v", req)
	}
	if !strings.HasSuffix(req.DiscoveryEndpointURL, "/.well-known/openid-configuration") {
		t.Fatalf("discoveryEndpointUrl = %q", req.DiscoveryEndpointURL)
	}
	if !strings.HasSuffix(req.JWKEndpointURL, "/protocol/openid-connect/certs") {
		t.Fatalf("jwkEndpointUrl = %q", req.JWKEndpointURL)
	}
	if req.UserIdentifier != "preferred_username" {
		t.Fatalf("userIdentifier = %q", req.UserIdentifier)
	}
	if req.TokenEndpointAuthMethod != "basic" {
		t.Fatalf("tokenEndpointAuthMethod = %q", req.TokenEndpointAuthMethod)
	}
	if len(req.Certificates) != 1 || req.Certificates[0].Alias != "keycloak-route-1" {
		t.Fatalf("certificates not forwarded: %+v", req.Certificates)
	}
}

func TestBuildSAMLSPRequestDefaults(t *testing.T) {
	opts := &masAuthApplyOptions{samlProviderID: defaultMASAuthSAMLID}
	req := opts.buildSAMLSPRequest()
	if req.Issuer != defaultMASAuthSAMLID {
		t.Fatalf("issuer = %q", req.Issuer)
	}
	if req.ServiceProviderName != defaultMASAuthSAMLSPName {
		t.Fatalf("serviceProviderName = %q", req.ServiceProviderName)
	}
	if req.NameIDFormat != defaultMASAuthSAMLNameID {
		t.Fatalf("nameIDFormat = %q", req.NameIDFormat)
	}
	if req.SPInitiatedLogout == nil || !*req.SPInitiatedLogout {
		t.Fatalf("spInitiatedLogout = %v", req.SPInitiatedLogout)
	}
}

func TestToMASAdminCertsCopiesEntries(t *testing.T) {
	in := []certificateEntry{
		{Alias: "a-1", CRT: "x"},
		{Alias: "b-2", CRT: "y"},
	}
	out := toMASAdminCerts(in)
	if len(out) != 2 {
		t.Fatalf("len = %d", len(out))
	}
	if out[0] != (masadmin.Certificate{Alias: "a-1", CRT: "x"}) {
		t.Fatalf("first = %+v", out[0])
	}
	if got := toMASAdminCerts(nil); got == nil || len(got) != 0 {
		t.Fatalf("nil/empty input must map to non-nil empty slice (got %v)", got)
	}
}

func TestConditionSummaryFormatsConditions(t *testing.T) {
	if got := conditionSummary(nil); !strings.Contains(got, "no conditions") {
		t.Fatalf("nil case = %q", got)
	}
	resp := &masadmin.IDPConfigResponse{Status: masadmin.IDPStatus{Conditions: []masadmin.IDPCondition{
		{Type: "Ready", Status: "False", Reason: "Pending"},
		{Type: "Healthy", Status: "True", Reason: ""},
	}}}
	got := conditionSummary(resp)
	if !strings.Contains(got, "Ready=False(Pending)") || !strings.Contains(got, "Healthy=True") {
		t.Fatalf("summary = %q", got)
	}
}

func TestSelfRegConfigYAMLEmitsRequestedMappingsAndWorkspace(t *testing.T) {
	opts := &masAuthApplyOptions{selfRegWorkspaceID: "demoap"}

	oidc := opts.selfRegConfigYAML(oidcSelfRegMappings)
	if !strings.Contains(oidc, "id: preferred_username") {
		t.Fatalf("OIDC mappings missing preferred_username: %q", oidc)
	}
	if !strings.Contains(oidc, "id: demoap") {
		t.Fatalf("workspace id not threaded through: %q", oidc)
	}

	ldap := opts.selfRegConfigYAML(ldapSelfRegMappings)
	if !strings.Contains(ldap, "id: uid") || !strings.Contains(ldap, "email: mail") || !strings.Contains(ldap, "displayName: cn") {
		t.Fatalf("LDAP mappings should use LDAP attribute names, got: %q", ldap)
	}
	if strings.Contains(ldap, "preferred_username") {
		t.Fatalf("LDAP entry should NOT contain OIDC claim names: %q", ldap)
	}
}

func TestSAMLIssuerURLMatchesLibertyDerivedIssuer(t *testing.T) {
	got := samlIssuerURL("auth.mas91.apps.example.com", "mas-est-saml-sp")
	want := "https://auth.mas91.apps.example.com/ibm/saml20/mas-est-saml-sp"
	if got != want {
		t.Fatalf("samlIssuerURL = %q, want %q", got, want)
	}
}

func TestKeycloakMASClientScriptIncludesSAMLQuirks(t *testing.T) {
	script := keycloakMASClientScript(map[string]string{
		"KC_ADMIN_USER":      "admin",
		"KC_ADMIN_PASSWORD":  "secret",
		"KC_SERVICE":         "http://kc:8080",
		"KC_REALM":           "maximo",
		"CONFIGURE_OIDC":     "false",
		"OIDC_CLIENT_ID":     "",
		"OIDC_CLIENT_SECRET": "",
		"OIDC_REDIRECT_URI":  "",
		"CONFIGURE_SAML":     "true",
		"SAML_CLIENT_ID":     "https://auth.mas91.apps.example.com/ibm/saml20/mas-est-saml-sp",
		"SAML_SP_NAME":       "mas-est-saml-sp",
		"SAML_REDIRECT_URI":  "https://auth.mas91.apps.example.com/*",
		"SAML_SLO_URL":       "https://auth.mas91.apps.example.com/ibm/saml20/mas-est-saml-sp/slo",
	})
	for _, want := range []string{
		// URL-form clientId, not the bare SP name
		`SAML_CLIENT_ID='https://auth.mas91.apps.example.com/ibm/saml20/mas-est-saml-sp'`,
		// SLO URLs in the client attributes block
		`"saml_single_logout_service_url_post": "${SAML_SLO_URL}"`,
		`"saml_single_logout_service_url_redirect": "${SAML_SLO_URL}"`,
		// All four selfreg-aligned protocol mappers
		"ensure_saml_mapper preferred_username username",
		"ensure_saml_mapper email email",
		"ensure_saml_mapper given_name firstName",
		"ensure_saml_mapper family_name lastName",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q\n---script---\n%s", want, script)
		}
	}
}

func TestOIDCConnectionSecretDataPopulatesAllEndpoints(t *testing.T) {
	opts := &masAuthApplyOptions{
		namespace:      "mas-est",
		realm:          "maximo",
		masAuthHost:    "auth.mas91.apps.example.com",
		oidcProviderID: defaultMASAuthOIDCID,
	}
	kc := keycloakMASClients{
		BaseURL:          "https://keycloak.apps.example.com",
		OIDCClientID:     "mas-est-oidc",
		OIDCClientSecret: "shhh",
	}

	data := opts.oidcConnectionSecretData(kc)
	expected := map[string]string{
		"issuerUrl":             "https://keycloak.apps.example.com/realms/maximo",
		"discoveryUrl":          "https://keycloak.apps.example.com/realms/maximo/.well-known/openid-configuration",
		"authorizationEndpoint": "https://keycloak.apps.example.com/realms/maximo/protocol/openid-connect/auth",
		"tokenEndpoint":         "https://keycloak.apps.example.com/realms/maximo/protocol/openid-connect/token",
		"jwksEndpoint":          "https://keycloak.apps.example.com/realms/maximo/protocol/openid-connect/certs",
		"clientId":              "mas-est-oidc",
		"clientSecret":          "shhh",
		"realm":                 "maximo",
		"redirectUri":           "https://auth.mas91.apps.example.com/oidcclient/redirect/mas-est-oidc",
	}
	for k, want := range expected {
		if data[k] != want {
			t.Fatalf("oidcConnectionSecretData[%q] = %q, want %q", k, data[k], want)
		}
	}
}

func TestSAMLConnectionSecretDataUsesURLFormEntityID(t *testing.T) {
	opts := &masAuthApplyOptions{
		realm:          "maximo",
		masAuthHost:    "auth.mas91.apps.example.com",
		samlProviderID: defaultMASAuthSAMLID,
	}
	kc := keycloakMASClients{
		BaseURL:      "https://keycloak.apps.example.com",
		SAMLMetadata: "<EntityDescriptor />",
	}

	data := opts.samlConnectionSecretData(kc)
	// entityId must match Liberty's <saml:Issuer> URL form, NOT the bare SP
	// name — see memory: saml-login-invalid-request and TestSAMLIssuerURL…
	wantEntity := "https://auth.mas91.apps.example.com/ibm/saml20/mas-est-saml-sp"
	if data["entityId"] != wantEntity {
		t.Fatalf("entityId = %q, want %q", data["entityId"], wantEntity)
	}
	if data["acsUrl"] != wantEntity {
		t.Fatalf("acsUrl = %q, want %q", data["acsUrl"], wantEntity)
	}
	if data["sloUrl"] != wantEntity+"/slo" {
		t.Fatalf("sloUrl = %q", data["sloUrl"])
	}
	if data["idpMetadataUrl"] != "https://keycloak.apps.example.com/realms/maximo/protocol/saml/descriptor" {
		t.Fatalf("idpMetadataUrl = %q", data["idpMetadataUrl"])
	}
	if data["idpMetadata"] != "<EntityDescriptor />" {
		t.Fatalf("idpMetadata = %q", data["idpMetadata"])
	}
	if data["nameIdFormat"] != defaultMASAuthSAMLNameID {
		t.Fatalf("nameIdFormat = %q", data["nameIdFormat"])
	}
}

func TestSAMLConnectionSecretDataOmitsEmptyMetadata(t *testing.T) {
	opts := &masAuthApplyOptions{
		masAuthHost:    "auth.mas91.apps.example.com",
		samlProviderID: defaultMASAuthSAMLID,
	}
	data := opts.samlConnectionSecretData(keycloakMASClients{SAMLMetadata: "   "})
	if _, present := data["idpMetadata"]; present {
		t.Fatalf("idpMetadata should be omitted when blank, got %q", data["idpMetadata"])
	}
}

func TestJoinCertificatePEMConcatenates(t *testing.T) {
	got := joinCertificatePEM([]certificateEntry{
		{Alias: "a.1", CRT: "-----BEGIN CERTIFICATE-----\nAAA\n-----END CERTIFICATE-----"},
		{Alias: "a.2", CRT: "\n-----BEGIN CERTIFICATE-----\nBBB\n-----END CERTIFICATE-----\n"},
	})
	if !strings.Contains(got, "AAA") || !strings.Contains(got, "BBB") {
		t.Fatalf("joined = %q", got)
	}
	if got := joinCertificatePEM(nil); got != "" {
		t.Fatalf("nil input must produce empty string, got %q", got)
	}
}

func TestCertificatesFromPEMSplitsChain(t *testing.T) {
	raw := `-----BEGIN CERTIFICATE-----
one
-----END CERTIFICATE-----
-----BEGIN CERTIFICATE-----
two
-----END CERTIFICATE-----`

	certs := certificatesFromPEM(raw, "Keycloak Route Cert")
	if len(certs) != 2 {
		t.Fatalf("len(certs) = %d", len(certs))
	}
	if certs[0].Alias != "keycloak.route.cert.1" {
		t.Fatalf("alias = %q", certs[0].Alias)
	}
}
