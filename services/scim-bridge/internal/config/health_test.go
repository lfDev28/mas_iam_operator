package config

import "testing"

func TestHealthDefaultsAndEnvOverrides(t *testing.T) {
	cfg := DefaultSettings()
	if cfg.Bridge.HealthPort != 8081 || cfg.Bridge.HealthStaleCycles != 5 {
		t.Fatalf("unexpected health defaults: port=%d stale=%d", cfg.Bridge.HealthPort, cfg.Bridge.HealthStaleCycles)
	}
	t.Setenv("SCIM_BRIDGE_HEALTH_PORT", "9090")
	t.Setenv("SCIM_BRIDGE_HEALTH_STALE_CYCLES", "3")
	if err := ApplyEnvOverrides(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Bridge.HealthPort != 9090 || cfg.Bridge.HealthStaleCycles != 3 {
		t.Fatalf("env overrides not applied: %+v", cfg.Bridge)
	}
	t.Setenv("SCIM_BRIDGE_HEALTH_PORT", "nope")
	if err := ApplyEnvOverrides(&cfg); err == nil {
		t.Fatalf("expected error for non-numeric health port")
	}
}
