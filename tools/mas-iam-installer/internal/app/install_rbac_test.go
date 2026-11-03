package app

import (
	"strings"
	"testing"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/preflight"
)

func accessResources(checks []preflight.AccessCheck) []string {
	out := []string{}
	for _, check := range checks {
		out = append(out, check.Verb+" "+check.Resource)
	}
	return out
}

func TestInstallRequiredAccess(t *testing.T) {
	iam := config.DefaultInstallConfig()
	iam.Components = []string{config.InstallComponentLDAP}

	launcher := strings.Join(accessResources(installRequiredAccess(iam, true)), "|")
	for _, want := range []string{
		"create clusterroles.rbac.authorization.k8s.io",
		"create clusterrolebindings.rbac.authorization.k8s.io",
		"create customresourcedefinitions.apiextensions.k8s.io",
		"create catalogsources.operators.coreos.com",
		"create securitycontextconstraints.security.openshift.io",
	} {
		if !strings.Contains(launcher, want) {
			t.Errorf("in-cluster launcher checks missing %q: %s", want, launcher)
		}
	}

	smtp := config.DefaultInstallConfig()
	smtp.Components = []string{config.InstallComponentSMTP}
	if checks := installRequiredAccess(smtp, false); len(checks) != 0 {
		t.Fatalf("a --local SMTP-only install creates nothing cluster-scoped, got %v", accessResources(checks))
	}
	for _, check := range installRequiredAccess(iam, true) {
		if check.Resource == "catalogsources.operators.coreos.com" && check.Namespace != "openshift-marketplace" {
			t.Fatalf("CatalogSource check must target openshift-marketplace, got %q", check.Namespace)
		}
	}
}

// The Job's own preflight runs these checks as the installer service
// account. Every one must be granted by the installer ClusterRole, or the
// in-cluster install would fail its own preflight. RBAC for the Job comes from
// the launcher, so this is the place to catch a mismatch.
func TestInstallerClusterRoleSatisfiesJobPreflight(t *testing.T) {
	cfg := config.DefaultInstallConfig()
	cfg.Components = []string{config.InstallComponentLDAP, config.InstallComponentKeycloak, config.InstallComponentSCIM}
	rules := installerClusterRoleManifest()["rules"].([]map[string]any)

	for _, check := range installRequiredAccess(cfg, false) {
		resource, group, _ := strings.Cut(check.Resource, ".")
		granted := false
		for _, rule := range rules {
			if _, named := rule["resourceNames"]; named {
				continue
			}
			if containsString(rule["apiGroups"].([]string), group) &&
				containsString(rule["resources"].([]string), resource) &&
				containsString(rule["verbs"].([]string), check.Verb) {
				granted = true
				break
			}
		}
		if !granted {
			t.Errorf("installer ClusterRole does not grant %q, which the Job's preflight checks", check.String())
		}
	}
}
