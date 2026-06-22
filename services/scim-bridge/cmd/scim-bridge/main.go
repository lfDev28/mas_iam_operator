package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/bridge"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	cfg := config.DefaultSettings()
	if err := config.ApplyEnvOverrides(&cfg); err != nil {
		return err
	}

	fs := flag.NewFlagSet("scim-bridge", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	fs.StringVar(&cfg.Keycloak.BaseURL, "keycloak-base-url", cfg.Keycloak.BaseURL, "Keycloak Admin REST base URL (https://keycloak.example.com)")
	fs.StringVar(&cfg.Keycloak.Realm, "keycloak-realm", cfg.Keycloak.Realm, "Keycloak realm to monitor")
	fs.StringVar(&cfg.Keycloak.ClientID, "keycloak-client-id", cfg.Keycloak.ClientID, "Service account client ID")
	fs.StringVar(&cfg.Keycloak.ClientSecret, "keycloak-client-secret", cfg.Keycloak.ClientSecret, "Service account client secret")
	fs.BoolVar(&cfg.Keycloak.InsecureSkipVerify, "keycloak-insecure-skip-verify", cfg.Keycloak.InsecureSkipVerify, "Skip TLS verification when calling Keycloak (dev only)")
	fs.BoolVar(&cfg.Keycloak.IncludeFederatedUsers, "keycloak-include-federated-users", cfg.Keycloak.IncludeFederatedUsers, "Include LDAP-federated users in sync (default: skip them so LDAP self-reg isn't shadowed)")

	fs.StringVar(&cfg.MAS.BaseURL, "mas-base-url", cfg.MAS.BaseURL, "MAS SCIM base URL (https://api.<mas>/scim/v2)")
	fs.StringVar(&cfg.MAS.ProfileID, "mas-profile-id", cfg.MAS.ProfileID, "MAS SCIM profile ID applied to pushes")
	profileMap := ""
	profileMapJSON := ""
	fs.StringVar(&profileMap, "mas-profile-map", profileMap, "Optional mapping of masProfile label -> MAS profile ID (comma-separated key=value list)")
	fs.StringVar(&profileMapJSON, "mas-profile-map-json", profileMapJSON, "Optional mapping of masProfile label -> MAS profile ID (JSON object)")
	fs.BoolVar(&cfg.MAS.ProfileRequireLabel, "mas-profile-require-label", cfg.MAS.ProfileRequireLabel, "Require masProfile label to be present and mapped; when true, missing/unmapped users are skipped")
	fs.StringVar(&cfg.MAS.AuthType, "mas-auth-type", cfg.MAS.AuthType, "MAS auth type: api-key or jwt")
	fs.StringVar(&cfg.MAS.Token, "mas-token", cfg.MAS.Token, "MAS API key or JWT")
	fs.StringVar(&cfg.MAS.APITokenName, "mas-api-token-name", cfg.MAS.APITokenName, "MAS basic auth username for /v1/authenticate (optional if mas-token set)")
	fs.StringVar(&cfg.MAS.APITokenValue, "mas-api-token-value", cfg.MAS.APITokenValue, "MAS basic auth password for /v1/authenticate (optional if mas-token set)")
	fs.BoolVar(&cfg.MAS.InsecureSkipVerify, "mas-insecure-skip-verify", cfg.MAS.InsecureSkipVerify, "Skip TLS verification for MAS API calls (dev only)")

	fs.StringVar(&cfg.Bridge.Mode, "bridge-mode", cfg.Bridge.Mode, "Sync mode: poll, run-once, backfill, or hybrid")
	fs.DurationVar(&cfg.Bridge.PollInterval, "bridge-poll-interval", cfg.Bridge.PollInterval, "Polling interval for reconciliation (poll/hybrid)")
	fs.StringVar(&cfg.Bridge.StateBackend, "bridge-state-backend", cfg.Bridge.StateBackend, "State backend: memory, filesystem, or postgresql")
	fs.StringVar(&cfg.Bridge.StatePath, "bridge-state-path", cfg.Bridge.StatePath, "Path to correlation state file (filesystem backend)")
	fs.BoolVar(&cfg.Bridge.DryRun, "bridge-dry-run", cfg.Bridge.DryRun, "If true, skip MAS API calls and only log actions")
	fs.BoolVar(&cfg.Bridge.AllowUpdates, "bridge-allow-updates", cfg.Bridge.AllowUpdates, "Allow MAS updates; when false, updates are logged as skipped")
	fs.StringVar(&cfg.Bridge.LogLevel, "bridge-log-level", cfg.Bridge.LogLevel, "Bridge log level: debug, info, warn, or error")
	fs.BoolVar(&cfg.Bridge.PayloadLogging, "bridge-payload-logging", cfg.Bridge.PayloadLogging, "Log redacted outbound SCIM payloads (support/debug only)")

	includeUsernames := strings.Join(cfg.Bridge.IncludeUsernames, ",")
	includePrefix := cfg.Bridge.IncludeUsernamePrefix
	fs.StringVar(&includeUsernames, "include-usernames", includeUsernames, "Comma-separated usernames to include (optional)")
	fs.StringVar(&includePrefix, "include-username-prefix", includePrefix, "Only include users whose username starts with this prefix (optional)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg.Bridge.IncludeUsernames = config.ParseList(includeUsernames)
	cfg.Bridge.IncludeUsernamePrefix = includePrefix
	if profileMap != "" {
		mapping, err := config.ParseProfileMap(profileMap)
		if err != nil {
			return err
		}
		cfg.MAS.ProfileMap = mapping
	}
	if profileMapJSON != "" {
		mapping, err := config.ParseProfileMapJSON(profileMapJSON)
		if err != nil {
			return err
		}
		cfg.MAS.ProfileMap = mapping
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	app := bridge.NewApp(cfg)
	return app.Run(ctx)
}
