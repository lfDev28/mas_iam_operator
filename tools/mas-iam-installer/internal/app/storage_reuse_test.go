package app

import (
	"strings"
	"testing"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
)

func TestReconcileStorageClass(t *testing.T) {
	existing := map[string]string{
		"data-mas-est-iam-postgresql-0": "ocs-storagecluster-ceph-rbd",
		"unrelated":                     "gp3",
	}

	t.Run("no existing PVC keeps the request", func(t *testing.T) {
		class, note, warn := reconcileStorageClass("gp3", true, map[string]string{}, primaryStoragePVCs)
		if class != "gp3" || note != "" || warn {
			t.Fatalf("got %q %q %v", class, note, warn)
		}
	})
	t.Run("recommended class drifted: reuse quietly", func(t *testing.T) {
		class, note, warn := reconcileStorageClass("gp3-csi", false, existing, primaryStoragePVCs)
		if class != "ocs-storagecluster-ceph-rbd" || warn || !strings.Contains(note, "reusing") {
			t.Fatalf("got %q %q %v", class, note, warn)
		}
	})
	t.Run("explicit different class: reuse and warn", func(t *testing.T) {
		class, note, warn := reconcileStorageClass("gp3-csi", true, existing, primaryStoragePVCs)
		if class != "ocs-storagecluster-ceph-rbd" || !warn || !strings.Contains(note, "cannot be changed") {
			t.Fatalf("got %q %q %v", class, note, warn)
		}
	})
	t.Run("same class: nothing to say", func(t *testing.T) {
		class, note, warn := reconcileStorageClass("ocs-storagecluster-ceph-rbd", true, existing, primaryStoragePVCs)
		if class != "ocs-storagecluster-ceph-rbd" || note != "" || warn {
			t.Fatalf("got %q %q %v", class, note, warn)
		}
	})
	t.Run("openldap PVC counts when postgres has none", func(t *testing.T) {
		class, _, _ := reconcileStorageClass("", false, map[string]string{"mas-est-iam-openldap": "lvms"}, primaryStoragePVCs)
		if class != "lvms" {
			t.Fatalf("got %q", class)
		}
	})
}

func TestPickPVCStorageClassSkipsStatic(t *testing.T) {
	classes := []oc.StorageClass{
		{Name: "localblock", IsDefault: true, Provisioner: oc.StaticProvisioner},
		{Name: "gp3-csi", Provisioner: "ebs.csi.aws.com"},
	}
	if got, err := pickPVCStorageClass(classes); err != nil || got != "gp3-csi" {
		t.Fatalf("got %q, %v; want gp3-csi (static default skipped)", got, err)
	}
	if _, err := pickPVCStorageClass(classes[:1]); err == nil || !strings.Contains(err.Error(), "statically provisioned") {
		t.Fatalf("only-static err = %v", err)
	}
	if _, err := pickPVCStorageClass(nil); err == nil {
		t.Fatal("no classes must error")
	}
}
