package app

import (
	"encoding/json"
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

// Regression: every re-run applied the Deployment without MINIO_DOMAIN and
// then re-added it, restarting MinIO twice. When the alias Services already
// exist, the first-pass Deployment must be identical to the one
// enableVirtualHostStyle applies, so the second apply is a no-op.
func TestMinIOFirstPassMatchesVirtualHostDeploymentOnReRun(t *testing.T) {
	opts := &minioInstallOptions{
		namespace:      "mas-est",
		name:           "mas-minio",
		image:          "img",
		rootSecretName: "mas-minio-root",
	}
	deploymentOf := func(manifests []map[string]any) map[string]any {
		for _, manifest := range manifests {
			if manifest["kind"] == "Deployment" {
				return manifest
			}
		}
		t.Fatal("no Deployment in installManifests")
		return nil
	}
	rerun, _ := json.Marshal(deploymentOf(opts.installManifests(true)))
	final, _ := json.Marshal(minioDeploymentManifest(opts, true))
	if string(rerun) != string(final) {
		t.Fatalf("re-run first pass differs from the virtual-host Deployment; MinIO would roll twice\nfirst: %s\nfinal: %s", rerun, final)
	}
	fresh, _ := json.Marshal(deploymentOf(opts.installManifests(false)))
	if strings.Contains(string(fresh), "MINIO_DOMAIN") {
		t.Fatal("a fresh install keeps the original two-step: no MINIO_DOMAIN before the alias Services exist")
	}
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
