package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromFileUpgradesLegacyProfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte(`{"kc1":"mas1"}`), 0o644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	entries, err := LoadFromFile(path, "default-profile")
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	entry := entries["kc1"]
	if entry.ProfileID != "default-profile" {
		t.Fatalf("expected default profile applied, got %q", entry.ProfileID)
	}
}

func TestLoadFromFileAddsProfileID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte(`{"kc1":{"masID":"mas1","status":"ok"}}`), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	entries, err := LoadFromFile(path, "profile-x")
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	entry := entries["kc1"]
	if entry.ProfileID != "profile-x" {
		t.Fatalf("expected default profile applied, got %q", entry.ProfileID)
	}
}

func TestWriteToFileNormalizesProfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	entries := map[string]Entry{
		"kc1": {MASID: "mas1", Status: StatusOK},
	}
	if err := WriteToFile(path, entries, "profile-y"); err != nil {
		t.Fatalf("write state: %v", err)
	}

	reloaded, err := LoadFromFile(path, "profile-y")
	if err != nil {
		t.Fatalf("reload state: %v", err)
	}
	if reloaded["kc1"].ProfileID != "profile-y" {
		t.Fatalf("expected profile set on write, got %q", reloaded["kc1"].ProfileID)
	}
}
