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
