package installer

import (
	"testing"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
)

func TestIAMInstallComponentsFiltersExternalServices(t *testing.T) {
	got := IAMInstallComponents([]string{
		config.InstallComponentSMTP,
		config.InstallComponentSCIM,
		config.InstallComponentS3,
	})
	want := []string{
		config.InstallComponentLDAP,
		config.InstallComponentKeycloak,
		config.InstallComponentSCIM,
	}

	if len(got) != len(want) {
		t.Fatalf("IAMInstallComponents() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("IAMInstallComponents() = %v, want %v", got, want)
		}
	}
}
