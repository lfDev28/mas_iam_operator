package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
)

// PVCs whose storageClassName an install decides. A PVC's storageClassName
// is immutable, so once one of these exists, a re-run must keep using its
// class: re-picking the "recommended" class (which can change between runs,
// e.g. after an ODF install) made the operator's Helm upgrade or the MinIO
// apply fail on the immutable field.
var (
	// --storage-class: the IAM stack's PostgreSQL StatefulSet PVC and the
	// OpenLDAP PVC (operators/mas-iam-operator/helm-charts/mas-iam-stack).
	primaryStoragePVCs = []string{
		"data-" + config.DefaultIAMRelease + "-postgresql-0",
		config.DefaultIAMRelease + "-openldap",
	}
	// --scim-bridge-storage-class.
	scimBridgeStoragePVCs = []string{"scim-bridge-state"}
)

// existingPVCStorageClasses maps PVC name to storageClassName in namespace.
// Errors (namespace missing, no access) mean "nothing to reuse".
func existingPVCStorageClasses(ctx context.Context, client *oc.Client, namespace string) map[string]string {
	classes := map[string]string{}
	if strings.TrimSpace(namespace) == "" {
		return classes
	}
	pvcs, err := client.PVCs(ctx, namespace)
	if err != nil {
		return classes
	}
	for _, pvc := range pvcs {
		classes[pvc.Name] = pvc.StorageClass
	}
	return classes
}

// reconcileStorageClass returns the class to use given the requested one and
// the classes of PVCs that already exist. The first existing PVC in pvcNames
// wins. note is non-empty when the result differs from the request; warn is
// true when the user explicitly asked for a different class.
func reconcileStorageClass(requested string, explicit bool, existing map[string]string, pvcNames []string) (class, note string, warn bool) {
	requested = strings.TrimSpace(requested)
	for _, name := range pvcNames {
		current := strings.TrimSpace(existing[name])
		if current == "" {
			continue
		}
		if requested == current {
			return current, "", false
		}
		if explicit && requested != "" {
			return current, fmt.Sprintf("storage class %q was requested, but PVC %s already uses %q and a PVC's storage class cannot be changed; keeping %q. To move to %q, uninstall first (this deletes the data).",
				requested, name, current, current, requested), true
		}
		return current, fmt.Sprintf("reusing storage class %q of existing PVC %s", current, name), false
	}
	return requested, "", false
}
