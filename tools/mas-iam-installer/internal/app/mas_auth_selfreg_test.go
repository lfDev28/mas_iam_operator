package app

import (
	"reflect"
	"testing"
)

// Regression: the selfreg ConfigMap is server-side applied under one field
// manager, so a re-run with fewer providers deleted the other providers'
// entries unless every existing key is carried forward.
func TestMergeSelfRegDataPreservesProvidersOutsideThisRun(t *testing.T) {
	existing := map[string]string{
		"default-ldap": "old ldap",
		"default-oidc": "oidc entry",
		"default-saml": "saml entry",
		"customer-idp": "hand-written",
	}
	desired := map[string]string{
		"default-ldap": "new ldap",
	}
	merged, preserved := mergeSelfRegData(existing, desired)

	want := map[string]string{
		"default-ldap": "new ldap",
		"default-oidc": "oidc entry",
		"default-saml": "saml entry",
		"customer-idp": "hand-written",
	}
	if !reflect.DeepEqual(merged, want) {
		t.Fatalf("merged = %v, want %v", merged, want)
	}
	if !reflect.DeepEqual(preserved, []string{"customer-idp", "default-oidc", "default-saml"}) {
		t.Fatalf("preserved = %v", preserved)
	}
	if existing["default-ldap"] != "old ldap" {
		t.Fatal("mergeSelfRegData must not mutate the existing map")
	}
}

func TestMergeSelfRegDataWithoutExistingConfigMap(t *testing.T) {
	merged, preserved := mergeSelfRegData(nil, map[string]string{"default-oidc": "x"})
	if len(merged) != 1 || merged["default-oidc"] != "x" || len(preserved) != 0 {
		t.Fatalf("merged=%v preserved=%v", merged, preserved)
	}
}
