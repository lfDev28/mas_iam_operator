package preflight

import (
	"testing"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
)

func TestRankStorageClassesPrefersBlockOverCephfsDefault(t *testing.T) {
	ranking := RankStorageClasses([]oc.StorageClass{
		{Name: "ocs-storagecluster-cephfs", IsDefault: true},
		{Name: "ocs-external-storagecluster-ceph-rbd", IsDefault: false},
		{Name: "gp3-csi", IsDefault: false},
	})

	if ranking.Recommended != "ocs-external-storagecluster-ceph-rbd" {
		t.Fatalf("expected rbd class to be recommended, got %q", ranking.Recommended)
	}
	if ranking.Warning == "" {
		t.Fatal("expected warning when cephfs is default and rbd exists")
	}
}

func TestParseMASHostRequiresSCIMPath(t *testing.T) {
	_, err := ParseMASHost("https://api.example.com")
	if err == nil {
		t.Fatal("expected missing /scim/v2 path to fail")
	}
}

func TestParseMASHostReturnsHostname(t *testing.T) {
	host, err := ParseMASHost("https://api.example.com/scim/v2")
	if err != nil {
		t.Fatalf("expected valid MAS URL, got error: %v", err)
	}
	if host != "api.example.com" {
		t.Fatalf("expected host api.example.com, got %q", host)
	}
}
