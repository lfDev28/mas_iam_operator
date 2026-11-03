package app

import (
	"testing"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
)

func authOnlyConfig() config.InstallConfig {
	cfg := config.DefaultInstallConfig()
	cfg.Components = []string{config.InstallComponentLDAP, config.InstallComponentKeycloak}
	cfg.ConfigureMASAuth = true
	cfg.MASAuthInstanceID = "inst"
	return cfg
}

func TestResolveMASAuthAPITargetDerivesURLFromRoute(t *testing.T) {
	routes := []oc.MASRoute{
		{InstanceID: "other", Host: "api.other.example.com"},
		{InstanceID: "inst", Host: "api.inst.example.com", Namespace: "mas-inst-core", Name: "inst-api"},
	}
	cfg := authOnlyConfig()
	resolveMASAuthAPITarget(&cfg, routes)
	if cfg.MASBaseURL != "https://api.inst.example.com/scim/v2" {
		t.Fatalf("MASBaseURL = %q, want the auth instance's API route", cfg.MASBaseURL)
	}

	cfg = authOnlyConfig()
	cfg.MASBaseURL = "https://api.given.example.com/"
	resolveMASAuthAPITarget(&cfg, routes)
	if cfg.MASBaseURL != "https://api.given.example.com/scim/v2" {
		t.Fatalf("MASBaseURL = %q, want the given root normalised to /scim/v2", cfg.MASBaseURL)
	}

	cfg = authOnlyConfig()
	cfg.MASAuthUseCRApply = true
	resolveMASAuthAPITarget(&cfg, routes)
	if cfg.MASBaseURL != "" {
		t.Fatalf("CR path needs no MAS API URL, got %q", cfg.MASBaseURL)
	}
}

func TestApplyStoredMASAPICredentialsOnlyFillsBlanks(t *testing.T) {
	stored := map[string]string{
		config.EnvMASAPITokenName:  "stored-name",
		config.EnvMASAPITokenValue: "stored-value",
		config.EnvMASBaseURL:       "https://api.stored.example.com/scim/v2",
	}
	cfg := authOnlyConfig()
	cfg.MASAPITokenName = "flag-name"
	if !applyStoredMASAPICredentials(&cfg, stored) {
		t.Fatal("expected blanks to be filled")
	}
	if cfg.MASAPITokenName != "flag-name" {
		t.Fatalf("flag value overwritten: %q", cfg.MASAPITokenName)
	}
	if cfg.MASAPITokenValue != "stored-value" || cfg.MASBaseURL != stored[config.EnvMASBaseURL] {
		t.Fatalf("blanks not filled: %+v", cfg)
	}
	if applyStoredMASAPICredentials(&cfg, stored) {
		t.Fatal("nothing left to fill, want false")
	}
}

func TestUsesMASAdminAPI(t *testing.T) {
	cfg := authOnlyConfig()
	if !usesMASAdminAPI(cfg) {
		t.Fatal("auth-only Admin API path must use the MAS API")
	}
	cfg.MASAuthUseCRApply = true
	if usesMASAdminAPI(cfg) {
		t.Fatal("CR path without scim does not use the MAS API")
	}
	cfg.Components = append(cfg.Components, config.InstallComponentSCIM)
	if !usesMASAdminAPI(cfg) {
		t.Fatal("scim always uses the MAS API")
	}
}
