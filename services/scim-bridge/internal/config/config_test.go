package config

import "testing"

func TestResolveProfile(t *testing.T) {
	cfg := MASConfig{
		ProfileID: "default",
		ProfileMap: map[string]string{
			"users": "test1",
		},
	}

	if id, ok := cfg.ResolveProfile("users"); !ok || id != "test1" {
		t.Fatalf("expected mapped profile test1, got %q (ok=%v)", id, ok)
	}

	if id, ok := cfg.ResolveProfile("unknown"); !ok || id != "default" {
		t.Fatalf("expected fallback profile default, got %q (ok=%v)", id, ok)
	}

	cfg.ProfileRequireLabel = true
	if _, ok := cfg.ResolveProfile(""); ok {
		t.Fatalf("expected missing label to be rejected in strict mode")
	}
	if _, ok := cfg.ResolveProfile("missing"); ok {
		t.Fatalf("expected unmapped label to be rejected in strict mode")
	}
}

func TestParseProfileMap(t *testing.T) {
	mapping, err := ParseProfileMap("users=test1,management=mgmt1")
	if err != nil {
		t.Fatalf("parse profile map: %v", err)
	}
	if mapping["users"] != "test1" || mapping["management"] != "mgmt1" {
		t.Fatalf("unexpected mapping: %#v", mapping)
	}
}

func TestParseProfileMapJSON(t *testing.T) {
	mapping, err := ParseProfileMapJSON(`{"Users":"test1","management":"mgmt1"}`)
	if err != nil {
		t.Fatalf("parse profile map json: %v", err)
	}
	if mapping["users"] != "test1" || mapping["management"] != "mgmt1" {
		t.Fatalf("unexpected mapping: %#v", mapping)
	}
}
