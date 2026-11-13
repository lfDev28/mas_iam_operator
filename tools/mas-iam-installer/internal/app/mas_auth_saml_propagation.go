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

// Vars so tests can shorten them.
var (
	samlMetadataPollInterval  = 15 * time.Second
	samlMetadataProgressEvery = 2 * time.Minute
	// samlMetadataSyncGrace covers kubelet's ConfigMap volume sync into the
	// coreidp pod after MAS updates the ConfigMap (about 30s observed).
	samlMetadataSyncGrace = 60 * time.Second
)

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
	if metadata, ok := data[o.samlIdPMetadataKey()]; ok {
		return sameCerts(samlMetadataCerts(metadata), want), nil
	}
	// The key name ({IDPCfg name}_idpMetadata.xml) was measured on MAS 9.1;
	// if another version names it differently, match on content instead of
	// waiting out the timeout on every run.
	for key, metadata := range data {
		if strings.HasSuffix(key, "_idpMetadata.xml") && sameCerts(samlMetadataCerts(metadata), want) {
			return true, nil
		}
	}
	return false, nil
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
	start := time.Now()
	deadline := start.Add(samlMetadataPropagationTimeout)
	nextProgress := start.Add(samlMetadataProgressEvery)
	var lastErr error
	for {
		current, err := o.coreIDPSAMLCertsCurrent(ctx, client, want)
		lastErr = err // only the latest read matters for the final message
		if err == nil && current {
			// The ConfigMap is updated; kubelet syncs it into the coreidp
			// pod (up to about a minute) and Liberty then reloads the file.
			fmt.Fprintf(os.Stdout, "[mas-auth] MAS rendered the new SAML IdP metadata after %s; allowing %s for coreidp to load it\n",
				time.Since(start).Round(time.Second), samlMetadataSyncGrace)
			return sleepCtx(ctx, samlMetadataSyncGrace)
		}
		if now := time.Now(); now.After(nextProgress) && now.Before(deadline) {
			fmt.Fprintf(os.Stdout, "[mas-auth] still waiting for MAS to render the new SAML IdP metadata (%s so far)\n", now.Sub(start).Round(time.Second))
			nextProgress = now.Add(samlMetadataProgressEvery)
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
