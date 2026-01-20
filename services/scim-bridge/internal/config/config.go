package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

const envPrefix = "SCIM_BRIDGE"

// Settings captures the top-level configuration for the bridge.
type Settings struct {
	Keycloak KeycloakConfig
	MAS      MASConfig
	Bridge   BridgeConfig
}

// KeycloakConfig controls how we talk to the source realm.
type KeycloakConfig struct {
	BaseURL            string
	Realm              string
	ClientID           string
	ClientSecret       string
	CAFile             string
	InsecureSkipVerify bool
}

// MASConfig captures how we address MAS SCIM endpoints.
type MASConfig struct {
	BaseURL             string
	ProfileID           string
	ProfileMap          map[string]string
	ProfileRequireLabel bool
	CAFile              string
	AuthType            string
	Token               string
	APITokenName        string
	APITokenValue       string
	InsecureSkipVerify  bool
}

// BridgeConfig tunes runtime behavior for the reconciler.
type BridgeConfig struct {
	Mode                  string
	PollInterval          time.Duration
	StateBackend          string
	DryRun                bool
	StatePath             string
	AllowUpdates          bool
	IncludeUsernames      []string
	IncludeUsernamePrefix string
}

// DefaultSettings seeds sane defaults for the CLI/env overrides.
func DefaultSettings() Settings {
	return Settings{
		Keycloak: KeycloakConfig{
			Realm: "master",
		},
		MAS: MASConfig{
			AuthType:            "api-key",
			ProfileMap:          map[string]string{},
			ProfileRequireLabel: false,
		},
		Bridge: BridgeConfig{
			Mode:                  "poll",
			PollInterval:          5 * time.Minute,
			StateBackend:          "memory",
			DryRun:                true,
			StatePath:             "",
			AllowUpdates:          true,
			IncludeUsernames:      nil,
			IncludeUsernamePrefix: "",
		},
	}
}

// ApplyEnvOverrides mutates the provided settings based on SCIM_BRIDGE_* env vars.
func ApplyEnvOverrides(cfg *Settings) error {
	type override struct {
		key   string
		apply func(string) error
	}

	overrides := []override{
		{envName("KEYCLOAK_BASE_URL"), func(v string) error { cfg.Keycloak.BaseURL = v; return nil }},
		{envName("KEYCLOAK_REALM"), func(v string) error { cfg.Keycloak.Realm = v; return nil }},
		{envName("KEYCLOAK_CLIENT_ID"), func(v string) error { cfg.Keycloak.ClientID = v; return nil }},
		{envName("KEYCLOAK_CLIENT_SECRET"), func(v string) error { cfg.Keycloak.ClientSecret = v; return nil }},
		{envName("KEYCLOAK_CA_FILE"), func(v string) error { cfg.Keycloak.CAFile = v; return nil }},
		{envName("KEYCLOAK_INSECURE_SKIP_VERIFY"), func(v string) error {
			cfg.Keycloak.InsecureSkipVerify = v == "1" || strings.EqualFold(v, "true")
			return nil
		}},
		{envName("MAS_BASE_URL"), func(v string) error { cfg.MAS.BaseURL = v; return nil }},
		{envName("MAS_PROFILE_ID"), func(v string) error { cfg.MAS.ProfileID = v; return nil }},
		{envName("MAS_PROFILE_MAP"), func(v string) error {
			mapping, err := parseProfileMapPairs(v)
			if err != nil {
				return err
			}
			if len(mapping) > 0 {
				cfg.MAS.ProfileMap = mapping
			}
			return nil
		}},
		{envName("MAS_PROFILE_MAP_JSON"), func(v string) error {
			mapping, err := parseProfileMapJSON(v)
			if err != nil {
				return err
			}
			if len(mapping) > 0 {
				cfg.MAS.ProfileMap = mapping
			}
			return nil
		}},
		{envName("MAS_PROFILE_REQUIRE_LABEL"), func(v string) error {
			cfg.MAS.ProfileRequireLabel = v == "1" || strings.EqualFold(v, "true")
			return nil
		}},
		{envName("MAS_CA_FILE"), func(v string) error { cfg.MAS.CAFile = v; return nil }},
		{envName("MAS_AUTH_TYPE"), func(v string) error { cfg.MAS.AuthType = strings.ToLower(v); return nil }},
		{envName("MAS_TOKEN"), func(v string) error { cfg.MAS.Token = v; return nil }},
		{envName("MAS_API_TOKEN_NAME"), func(v string) error { cfg.MAS.APITokenName = v; return nil }},
		{envName("MAS_API_TOKEN_VALUE"), func(v string) error { cfg.MAS.APITokenValue = v; return nil }},
		{envName("MAS_INSECURE_SKIP_VERIFY"), func(v string) error {
			cfg.MAS.InsecureSkipVerify = v == "1" || strings.EqualFold(v, "true")
			return nil
		}},
		{envName("BRIDGE_MODE"), func(v string) error { cfg.Bridge.Mode = strings.ToLower(v); return nil }},
		{envName("BRIDGE_POLL_INTERVAL"), func(v string) error {
			dur, err := time.ParseDuration(v)
			if err != nil {
				return err
			}
			cfg.Bridge.PollInterval = dur
			return nil
		}},
		{envName("BRIDGE_STATE_BACKEND"), func(v string) error { cfg.Bridge.StateBackend = strings.ToLower(v); return nil }},
		{envName("BRIDGE_STATE_PATH"), func(v string) error { cfg.Bridge.StatePath = v; return nil }},
		{envName("BRIDGE_DRY_RUN"), func(v string) error {
			cfg.Bridge.DryRun = v == "1" || strings.EqualFold(v, "true")
			return nil
		}},
		{envName("BRIDGE_ALLOW_UPDATES"), func(v string) error {
			cfg.Bridge.AllowUpdates = !(v == "0" || strings.EqualFold(v, "false"))
			return nil
		}},
		{envName("INCLUDE_USERNAMES"), func(v string) error { cfg.Bridge.IncludeUsernames = ParseList(v); return nil }},
		{envName("INCLUDE_USERNAME_PREFIX"), func(v string) error { cfg.Bridge.IncludeUsernamePrefix = v; return nil }},
	}

	for _, ov := range overrides {
		if value, ok := os.LookupEnv(ov.key); ok {
			if err := ov.apply(value); err != nil {
				return fmt.Errorf("env %s: %w", ov.key, err)
			}
		}
	}

	cfg.MAS.ProfileMap = normalizeProfileMap(cfg.MAS.ProfileMap)
	return nil
}

// Validate ensures required knobs are set before runtime.
func (s Settings) Validate() error {
	var problems []string
	if s.Keycloak.BaseURL == "" {
		problems = append(problems, "keycloak base URL is required")
	}
	if s.Keycloak.ClientID == "" {
		problems = append(problems, "keycloak client ID is required")
	}
	if s.Keycloak.ClientSecret == "" {
		problems = append(problems, "keycloak client secret is required")
	}
	if s.MAS.BaseURL == "" {
		problems = append(problems, "MAS base URL is required")
	}
	if s.MAS.ProfileID == "" {
		problems = append(problems, "MAS profile ID is required")
	}
	if s.MAS.Token == "" && (s.MAS.APITokenName == "" || s.MAS.APITokenValue == "") {
		problems = append(problems, "MAS token is required (or set MAS_API_TOKEN_NAME / MAS_API_TOKEN_VALUE for on-demand fetch)")
	}
	if s.Bridge.PollInterval <= 0 {
		problems = append(problems, "poll interval must be > 0")
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	if _, err := url.ParseRequestURI(s.Keycloak.BaseURL); err != nil {
		return fmt.Errorf("invalid Keycloak base URL: %w", err)
	}
	if _, err := url.ParseRequestURI(s.MAS.BaseURL); err != nil {
		return fmt.Errorf("invalid MAS base URL: %w", err)
	}
	switch strings.ToLower(s.MAS.AuthType) {
	case "api-key", "jwt":
	default:
		return fmt.Errorf("mas auth type must be 'api-key' or 'jwt'")
	}
	switch s.Bridge.Mode {
	case "poll", "event", "hybrid", "run-once", "backfill":
	default:
		return fmt.Errorf("bridge mode must be poll, event, hybrid, run-once, or backfill")
	}
	switch s.Bridge.StateBackend {
	case "memory", "filesystem", "postgresql":
	default:
		return fmt.Errorf("state backend must be memory, filesystem, or postgresql")
	}
	if s.Bridge.StateBackend == "filesystem" && s.Bridge.StatePath == "" {
		return fmt.Errorf("bridge.state_path is required when state backend is filesystem")
	}
	return nil
}

// ResolveProfile returns the MAS profile ID for the given label according to configuration.
// When ProfileRequireLabel is true, missing or unmapped labels are rejected (ok == false).
// When false, missing or unmapped labels fall back to the default ProfileID.
func (m MASConfig) ResolveProfile(label string) (profileID string, ok bool) {
	normalized := strings.ToLower(strings.TrimSpace(label))
	if normalized != "" {
		if id, exists := m.ProfileMap[normalized]; exists {
			return id, true
		}
		if m.ProfileRequireLabel {
			return "", false
		}
	}
	if m.ProfileRequireLabel && normalized == "" {
		return "", false
	}
	return m.ProfileID, true
}

func parseProfileMapPairs(raw string) (map[string]string, error) {
	mapping := make(map[string]string)
	if strings.TrimSpace(raw) == "" {
		return mapping, nil
	}
	pairs := strings.Split(raw, ",")
	for _, pair := range pairs {
		if strings.TrimSpace(pair) == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid profile map entry %q (expected key=value)", pair)
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if key == "" || val == "" {
			return nil, fmt.Errorf("profile map entry %q must include key and value", pair)
		}
		mapping[key] = val
	}
	return mapping, nil
}

func parseProfileMapJSON(raw string) (map[string]string, error) {
	mapping := make(map[string]string)
	if strings.TrimSpace(raw) == "" {
		return mapping, nil
	}
	if err := json.Unmarshal([]byte(raw), &mapping); err != nil {
		return nil, fmt.Errorf("parse profile map JSON: %w", err)
	}
	return mapping, nil
}

func normalizeProfileMap(mapping map[string]string) map[string]string {
	normalized := make(map[string]string, len(mapping))
	for k, v := range mapping {
		key := strings.ToLower(strings.TrimSpace(k))
		val := strings.TrimSpace(v)
		if key == "" || val == "" {
			continue
		}
		normalized[key] = val
	}
	return normalized
}

// ParseProfileMap parses a comma-separated key=value list into a normalized map.
func ParseProfileMap(raw string) (map[string]string, error) {
	mapping, err := parseProfileMapPairs(raw)
	if err != nil {
		return nil, err
	}
	return normalizeProfileMap(mapping), nil
}

// ParseProfileMapJSON parses a JSON object into a normalized map.
func ParseProfileMapJSON(raw string) (map[string]string, error) {
	mapping, err := parseProfileMapJSON(raw)
	if err != nil {
		return nil, err
	}
	return normalizeProfileMap(mapping), nil
}

func envName(suffix string) string {
	return fmt.Sprintf("%s_%s", envPrefix, suffix)
}

// ParseList normalizes a comma-separated list, trimming whitespace and dropping empties.
func ParseList(value string) []string {
	parts := strings.Split(value, ",")
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	return filtered
}
