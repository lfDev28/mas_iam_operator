package preflight

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCheckClusterPermissions(t *testing.T) {
	checks := []AccessCheck{
		{Verb: "create", Resource: "clusterroles.rbac.authorization.k8s.io", Why: "the installer ClusterRole"},
		{Verb: "create", Resource: "catalogsources.operators.coreos.com", Namespace: "openshift-marketplace", Why: "the CatalogSource"},
	}

	allowAll := func(ctx context.Context, verb, resource, namespace string) (bool, error) { return true, nil }
	if result := CheckClusterPermissions(context.Background(), allowAll, checks); result.Status != StatusOK {
		t.Fatalf("all allowed = %s: %s", result.Status, result.Message)
	}

	denyCatalog := func(ctx context.Context, verb, resource, namespace string) (bool, error) {
		return !strings.HasPrefix(resource, "catalogsources"), nil
	}
	result := CheckClusterPermissions(context.Background(), denyCatalog, checks)
	if result.Status != StatusError {
		t.Fatalf("denied = %s, want error", result.Status)
	}
	for _, want := range []string{"create catalogsources.operators.coreos.com in namespace openshift-marketplace", "the CatalogSource", "cluster-admin"} {
		if !strings.Contains(result.Message, want) {
			t.Fatalf("message missing %q:\n%s", want, result.Message)
		}
	}
	if strings.Contains(result.Message, "clusterroles") {
		t.Fatalf("message lists a permission the user holds:\n%s", result.Message)
	}

	flaky := func(ctx context.Context, verb, resource, namespace string) (bool, error) {
		return false, errors.New("connection refused")
	}
	if result := CheckClusterPermissions(context.Background(), flaky, checks); result.Status != StatusWarn {
		t.Fatalf("review errors = %s, want warn (never block on a flaky API)", result.Status)
	}
}
