package app

import (
	"bytes"
	"fmt"
	"strings"
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

func TestPhaseTrackerRecordsSuccessAndFailureEntries(t *testing.T) {
	buf := &bytes.Buffer{}
	tracker := newPhaseTracker(buf)

	if err := tracker.run("first", func() error { return nil }); err != nil {
		t.Fatalf("first phase returned error: %v", err)
	}
	wantErr := fmt.Errorf("boom")
	if err := tracker.run("second", func() error { return wantErr }); err != wantErr {
		t.Fatalf("second phase err = %v, want %v", err, wantErr)
	}

	if len(tracker.entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(tracker.entries))
	}
	if tracker.entries[0].label != "first" || !tracker.entries[0].success {
		t.Fatalf("entries[0] = %+v", tracker.entries[0])
	}
	if tracker.entries[1].label != "second" || tracker.entries[1].success {
		t.Fatalf("entries[1] = %+v", tracker.entries[1])
	}
	out := buf.String()
	if !strings.Contains(out, "✓ first") {
		t.Fatalf("expected ✓ first marker, got:\n%s", out)
	}
	if !strings.Contains(out, "✗ second") {
		t.Fatalf("expected ✗ second marker, got:\n%s", out)
	}
}

func TestPrintInstallSummaryListsConnectionRetrievalCommands(t *testing.T) {
	buf := &bytes.Buffer{}
	cfg := config.DefaultInstallConfig()
	cfg.Namespace = "mas-est"
	cfg.Components = []string{config.InstallComponentLDAP, config.InstallComponentKeycloak, config.InstallComponentS3, config.InstallComponentSMTP}
	cfg.ConfigureMASAuth = true
	cfg.MASAuthProviders = []string{config.MASAuthProviderLDAP, config.MASAuthProviderOIDC, config.MASAuthProviderSAML}

	printInstallSummary(buf, []phaseEntry{{label: "Install LDAP, Keycloak, SCIM bridge", success: true}}, 0, cfg)

	out := buf.String()
	wants := []string{
		"oc get secret mas-est-ldap-connection -n mas-est",
		"oc get secret mas-est-oidc-connection -n mas-est",
		"oc get secret mas-est-saml-connection -n mas-est",
		"oc get secret mas-est-s3-connection -n mas-est",
		"oc get configmap mas-est-smtp-connection -n mas-est",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("summary missing %q\n---\n%s", want, out)
		}
	}
}
