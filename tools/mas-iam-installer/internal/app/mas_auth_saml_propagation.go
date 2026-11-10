package app

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
)

// MAS's IDPCfg operator renders every SAML IdP's metadata into this one
// ConfigMap in the core namespace; coreidp mounts it as Liberty's
// idpMetadata files. It is the first place a new Keycloak signing
// certificate becomes visible to coreidp.
const masSAMLIdPConfigMapName = "mas-multi-saml-idp"

// samlMetadataPropagationTimeout bounds the wait for MAS to render new IdP
// metadata. Observed on MAS 9.1.14: the IDPCfg operator's SAML task waits
// 300s on a lock file it never removes, and a reconcile that started before
// the metadata PUT renders the old metadata, so two cycles (10+ minutes) are
// normal.
var samlMetadataPropagationTimeout = 15 * time.Minute

// samlMetadataPollInterval is a var so tests can shorten it.
var samlMetadataPollInterval = 15 * time.Second

func (o *masAuthApplyOptions) samlIdPMetadataKey() string {
	return o.masInstanceID + "-saml-" + o.samlProviderID + "-system_idpMetadata.xml"
}

var x509CertPattern = regexp.MustCompile(`<(?:[A-Za-z0-9_-]+:)?X509Certificate>\s*([^<]+?)\s*</`)

// samlMetadataCerts returns the distinct X509 certificates in SAML metadata,
// whitespace-free and sorted, so two metadata documents can be compared by
// the keys they publish rather than by their formatting.
func samlMetadataCerts(metadata string) []string {
	seen := map[string]bool{}
	var certs []string
	for _, m := range x509CertPattern.FindAllStringSubmatch(metadata, -1) {
		c := strings.Join(strings.Fields(m[1]), "")
		if c != "" && !seen[c] {
			seen[c] = true
			certs = append(certs, c)
		}
	}
	sort.Strings(certs)
	return certs
}

func sameCerts(a, b []string) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// coreIDPSAMLCertsCurrent reports whether MAS's rendered IdP metadata
// already publishes exactly the certificates in want. A missing ConfigMap or
// key is "not current", not an error: on a first install MAS has not
// rendered anything yet.
func (o *masAuthApplyOptions) coreIDPSAMLCertsCurrent(ctx context.Context, client *oc.Client, want []string) (bool, error) {
	data, err := client.ConfigMapData(ctx, o.masCoreNamespace, masSAMLIdPConfigMapName)
	if err != nil {
		if oc.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return sameCerts(samlMetadataCerts(data[o.samlIdPMetadataKey()]), want), nil
}

// waitForCoreIDPSAMLMetadata waits until MAS has rendered Keycloak's current
// SAML signing certificates for coreidp. The IDPCfg's Ready condition does
// not show this: it stays True from the previous apply while the old
// certificate is still served, so after a Keycloak key change (reinstall,
// realm key rotation) every SAML login failed with AIUOM0100E while the
// install reported success.
//
// Not fatal: MAS applies the metadata on a later reconcile by itself, and
// failing here would skip the SCIM linking that follows. The warning says
// what is wrong and how to check.
func (o *masAuthApplyOptions) waitForCoreIDPSAMLMetadata(ctx context.Context, client *oc.Client, want []string) error {
	where := fmt.Sprintf("configmap/%s key %s in %s", masSAMLIdPConfigMapName, o.samlIdPMetadataKey(), o.masCoreNamespace)
	fmt.Fprintf(os.Stdout, "[mas-auth] waiting up to %s for MAS to render Keycloak's new SAML signing certificate (%s)\n", samlMetadataPropagationTimeout, where)
	deadline := time.Now().Add(samlMetadataPropagationTimeout)
	var lastErr error
	for {
		current, err := o.coreIDPSAMLCertsCurrent(ctx, client, want)
		switch {
		case err == nil && current:
			fmt.Fprintln(os.Stdout, "[mas-auth] MAS rendered the new SAML IdP metadata for coreidp")
			return nil
		case err != nil:
			lastErr = err
		}
		if time.Now().After(deadline) {
			detail := "it still holds the previous certificate"
			if lastErr != nil {
				detail = "last read failed: " + lastErr.Error()
			}
			fmt.Fprintf(os.Stderr, "[mas-auth] WARN: MAS has not applied Keycloak's new SAML signing certificate after %s (%s: %s). "+
				"SAML logins fail with AIUOM0100E until it does. MAS's IDPCfg operator applies it on a later reconcile; "+
				"check with `oc get configmap %s -n %s -o yaml`, then run `make login-check`.\n",
				samlMetadataPropagationTimeout, where, detail, masSAMLIdPConfigMapName, o.masCoreNamespace)
			return nil
		}
		if err := sleepCtx(ctx, samlMetadataPollInterval); err != nil {
			return err
		}
	}
}
