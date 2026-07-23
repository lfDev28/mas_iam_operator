package config

import "testing"

func TestLoadInstallConfigPrefersMASESTNamespace(t *testing.T) {
	t.Setenv(EnvNamespace, "mas-est")
	t.Setenv(LegacyEnvNamespace, "iam")

	cfg := LoadInstallConfigFromEnv()

	if cfg.Namespace != "mas-est" {
		t.Fatalf("Namespace = %q, want mas-est", cfg.Namespace)
	}
}

func TestLoadInstallConfigFallsBackToLegacyMASIAMNamespace(t *testing.T) {
	t.Setenv(LegacyEnvNamespace, "legacy-iam")

	cfg := LoadInstallConfigFromEnv()

	if cfg.Namespace != "legacy-iam" {
		t.Fatalf("Namespace = %q, want legacy-iam", cfg.Namespace)
	}
}

func TestLoadWipeConfigPrefersMASESTSkipProfileDelete(t *testing.T) {
	t.Setenv(EnvSkipProfileDelete, "true")
	t.Setenv(LegacyEnvSkipProfileDelete, "false")

	cfg := LoadWipeConfigFromEnv()

	if !cfg.SkipProfileDelete {
		t.Fatal("SkipProfileDelete = false, want true")
	}
}

func TestLoadInstallConfigPrefersUninstallFirstOverWipeFirst(t *testing.T) {
	t.Setenv(EnvUninstallFirst, "true")
	t.Setenv(EnvWipeFirst, "false")
	t.Setenv(LegacyEnvWipeFirst, "false")

	cfg := LoadInstallConfigFromEnv()

	if !cfg.WipeFirst {
		t.Fatal("WipeFirst = false, want true")
	}
}

func TestLoadInstallConfigFallsBackToWipeFirst(t *testing.T) {
	t.Setenv(EnvWipeFirst, "true")
	t.Setenv(LegacyEnvWipeFirst, "false")

	cfg := LoadInstallConfigFromEnv()

	if !cfg.WipeFirst {
		t.Fatal("WipeFirst = false, want true")
	}
}

func TestNormalizeInstallComponentsAddsDependencies(t *testing.T) {
	components, err := NormalizeInstallComponents([]string{InstallComponentSCIM, InstallComponentS3, InstallComponentSMTP})
	if err != nil {
		t.Fatalf("NormalizeInstallComponents() error = %v", err)
	}

	want := []string{InstallComponentLDAP, InstallComponentKeycloak, InstallComponentSCIM, InstallComponentS3, InstallComponentSMTP}
	if len(components) != len(want) {
		t.Fatalf("components = %v, want %v", components, want)
	}
	for i := range want {
		if components[i] != want[i] {
			t.Fatalf("components = %v, want %v", components, want)
		}
	}
}

func TestValidateSMTPOnlyDoesNotRequireMASFields(t *testing.T) {
	cfg := DefaultInstallConfig()
	cfg.Components = []string{InstallComponentSMTP}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateLDAPOnlyDoesNotRequireMASFields(t *testing.T) {
	cfg := DefaultInstallConfig()
	cfg.Components = []string{InstallComponentLDAP}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateSCIMRequiresMASFields(t *testing.T) {
	cfg := DefaultInstallConfig()
	cfg.Components = []string{InstallComponentSCIM}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want missing MAS values")
	}
}

func TestValidateS3RequiresMASInstanceUnlessSkipped(t *testing.T) {
	cfg := DefaultInstallConfig()
	cfg.Components = []string{InstallComponentS3}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want missing MAS instance")
	}

	cfg.SkipS3MASConfig = true
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with SkipS3MASConfig error = %v", err)
	}
}

func TestValidateConfigureMASAuthRequiresIAMAndMASTarget(t *testing.T) {
	cfg := DefaultInstallConfig()
	cfg.Components = []string{InstallComponentSMTP}
	cfg.ConfigureMASAuth = true

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want missing IAM components and MAS target")
	}

	cfg.Components = []string{InstallComponentLDAP, InstallComponentKeycloak}
	cfg.MASAuthInstanceID = "demo"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with MAS auth target error = %v", err)
	}
}

func TestDefaultMASCoreNamespaceDerivesBothDirections(t *testing.T) {
	cfg := DefaultInstallConfig()
	cfg.MASInstanceID = "demo"
	cfg.DefaultMASCoreNamespace()
	if cfg.MASCoreNamespace != "mas-demo-core" {
		t.Fatalf("MASCoreNamespace = %q, want mas-demo-core", cfg.MASCoreNamespace)
	}

	cfg = DefaultInstallConfig()
	cfg.MASCoreNamespace = "mas-other-core"
	cfg.DefaultMASCoreNamespace()
	if cfg.MASInstanceID != "other" {
		t.Fatalf("MASInstanceID = %q, want other", cfg.MASInstanceID)
	}
}

func TestDefaultMASAuthTargetDerivesBothDirections(t *testing.T) {
	cfg := DefaultInstallConfig()
	cfg.MASAuthInstanceID = "demo"
	cfg.DefaultMASAuthTarget()
	if cfg.MASAuthCoreNamespace != "mas-demo-core" {
		t.Fatalf("MASAuthCoreNamespace = %q, want mas-demo-core", cfg.MASAuthCoreNamespace)
	}

	cfg = DefaultInstallConfig()
	cfg.MASAuthCoreNamespace = "mas-other-core"
	cfg.DefaultMASAuthTarget()
	if cfg.MASAuthInstanceID != "other" {
		t.Fatalf("MASAuthInstanceID = %q, want other", cfg.MASAuthInstanceID)
	}
}
