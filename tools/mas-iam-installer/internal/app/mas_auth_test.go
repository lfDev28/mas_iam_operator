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
	if ldap["userIdMap"] != "*:uid" {
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
