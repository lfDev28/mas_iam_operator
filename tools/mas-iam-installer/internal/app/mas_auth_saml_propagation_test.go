package app

import (
	"reflect"
	"testing"
)

func TestSAMLMetadataCerts(t *testing.T) {
	// Keycloak's descriptor uses the ds: prefix; MAS stores what we PUT, but
	// other IdPs use bare or dsig: prefixes and wrap the base64.
	keycloak := `<md:EntityDescriptor><md:IDPSSODescriptor><md:KeyDescriptor use="signing"><ds:KeyInfo><ds:X509Data>` +
		`<ds:X509Certificate>BBBB</ds:X509Certificate></ds:X509Data></ds:KeyInfo></md:KeyDescriptor>` +
		`<md:KeyDescriptor use="signing"><ds:KeyInfo><ds:X509Data><ds:X509Certificate>AAAA</ds:X509Certificate>` +
		`</ds:X509Data></ds:KeyInfo></md:KeyDescriptor></md:IDPSSODescriptor></md:EntityDescriptor>`
	reformatted := "<EntityDescriptor><X509Certificate>\n  AA\n  AA\n</X509Certificate>" +
		"<dsig:X509Certificate>BBBB</dsig:X509Certificate><X509Certificate>AAAA</X509Certificate></EntityDescriptor>"

	got := samlMetadataCerts(keycloak)
	if want := []string{"AAAA", "BBBB"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("samlMetadataCerts(keycloak) = %v, want %v", got, want)
	}
	if !sameCerts(got, samlMetadataCerts(reformatted)) {
		t.Fatalf("formatting, prefixes and duplicates must not matter: %v vs %v", got, samlMetadataCerts(reformatted))
	}
}

func TestSameCerts(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want bool
	}{
		{"equal", []string{"A", "B"}, []string{"A", "B"}, true},
		{"rotated key", []string{"A"}, []string{"B"}, false},
		{"key added", []string{"A"}, []string{"A", "B"}, false},
		{"nothing rendered yet", nil, []string{"A"}, false},
		{"no certs on either side is not a match", nil, nil, false},
	}
	for _, tc := range cases {
		if got := sameCerts(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: sameCerts(%v, %v) = %v, want %v", tc.name, tc.a, tc.b, got, tc.want)
		}
	}
}

func TestSAMLIdPMetadataKey(t *testing.T) {
	o := &masAuthApplyOptions{masInstanceID: "mas91", samlProviderID: "default"}
	if got, want := o.samlIdPMetadataKey(), "mas91-saml-default-system_idpMetadata.xml"; got != want {
		t.Fatalf("key = %q, want %q (the IDPCfg operator's {resourceName}_idpMetadata.xml)", got, want)
	}
}
