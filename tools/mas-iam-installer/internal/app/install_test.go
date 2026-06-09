package app

import (
	"testing"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
)

func TestInstallLogComponentPrefersSelectedRuntime(t *testing.T) {
	tests := []struct {
		name       string
		components []string
		want       string
	}{
		{
			name:       "scim",
			components: []string{config.InstallComponentSCIM, config.InstallComponentSMTP},
			want:       "bridge",
		},
		{
			name:       "smtp only",
			components: []string{config.InstallComponentSMTP},
			want:       "smtp",
		},
		{
			name:       "s3 only",
			components: []string{config.InstallComponentS3},
			want:       "minio",
		},
		{
			name:       "ldap only",
			components: []string{config.InstallComponentLDAP},
			want:       "openldap",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultInstallConfig()
			cfg.Components = tt.components
			if got := installLogComponent(cfg); got != tt.want {
				t.Fatalf("installLogComponent() = %q, want %q", got, tt.want)
			}
		})
	}
}
