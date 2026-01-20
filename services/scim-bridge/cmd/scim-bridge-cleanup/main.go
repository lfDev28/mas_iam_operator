package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/config"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/mas"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/state"
)

type errorEntry struct {
	KeycloakID string
	State      state.Entry
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg := config.DefaultSettings()
	if err := config.ApplyEnvOverrides(&cfg); err != nil {
		return err
	}

	fs := flag.NewFlagSet("scim-bridge-cleanup", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	fs.StringVar(&cfg.MAS.BaseURL, "mas-base-url", cfg.MAS.BaseURL, "MAS SCIM base URL (https://api.<mas>/scim/v2)")
	fs.StringVar(&cfg.MAS.AuthType, "mas-auth-type", cfg.MAS.AuthType, "MAS auth type: api-key or jwt")
	fs.StringVar(&cfg.MAS.Token, "mas-token", cfg.MAS.Token, "MAS API key or JWT")
	fs.StringVar(&cfg.MAS.APITokenName, "mas-api-token-name", cfg.MAS.APITokenName, "MAS basic auth username for /v1/authenticate (optional if mas-token set)")
	fs.StringVar(&cfg.MAS.APITokenValue, "mas-api-token-value", cfg.MAS.APITokenValue, "MAS basic auth password for /v1/authenticate (optional if mas-token set)")
	fs.BoolVar(&cfg.MAS.InsecureSkipVerify, "mas-insecure-skip-verify", cfg.MAS.InsecureSkipVerify, "Skip TLS verification for MAS API calls (dev only)")
	fs.StringVar(&cfg.Bridge.StatePath, "state-path", cfg.Bridge.StatePath, "Path to correlation state file (filesystem backend)")
	fs.StringVar(&cfg.MAS.ProfileID, "mas-profile-id", cfg.MAS.ProfileID, "Default MAS SCIM profile ID (used for legacy state entries)")

	deleteFlag := fs.Bool("delete", false, "When set, delete MAS users for entries marked status=error and remove them from state")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}

	if cfg.Bridge.StatePath == "" {
		return fmt.Errorf("state-path is required")
	}

	entries, err := state.LoadFromFile(cfg.Bridge.StatePath, cfg.MAS.ProfileID)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	errorEntries := collectErrorEntries(entries)
	if len(errorEntries) == 0 {
		fmt.Println("no status=error entries found in state")
		return nil
	}

	fmt.Printf("found %d error entries:\n", len(errorEntries))
	for _, e := range errorEntries {
		fmt.Printf("- kc_id=%s username=%s mas_id=%s profile=%s last_error=%s\n", e.KeycloakID, e.State.Username, e.State.MASID, e.State.ProfileID, e.State.LastError)
	}

	if !*deleteFlag {
		fmt.Println("dry-run: no deletes issued (use --delete to remove MAS users and clear state entries)")
		return nil
	}

	masClient, err := buildMASClient(cfg.MAS)
	if err != nil {
		return err
	}

	ctx := context.Background()
	deleted := 0
	for _, e := range errorEntries {
		fmt.Printf("deleting mas user kc_id=%s username=%s mas_id=%s profile=%s\n", e.KeycloakID, e.State.Username, e.State.MASID, e.State.ProfileID)
		if err := masClient.DeleteUser(ctx, e.State.ProfileID, e.State.MASID); err != nil {
			fmt.Printf("delete failed kc_id=%s mas_id=%s error=%v\n", e.KeycloakID, e.State.MASID, err)
			continue
		}
		delete(entries, e.KeycloakID)
		deleted++
	}

	if err := state.WriteToFile(cfg.Bridge.StatePath, entries, cfg.MAS.ProfileID); err != nil {
		return fmt.Errorf("write state: %w", err)
	}

	fmt.Printf("cleanup complete: deleted %d entries, %d remaining in state\n", deleted, len(entries))
	return nil
}

func collectErrorEntries(entries map[string]state.Entry) []errorEntry {
	list := make([]errorEntry, 0)
	for id, entry := range entries {
		if entry.Status == state.StatusError {
			list = append(list, errorEntry{KeycloakID: id, State: entry})
		}
	}
	return list
}

func buildMASClient(cfg config.MASConfig) (*mas.Client, error) {
	if cfg.BaseURL == "" || cfg.ProfileID == "" {
		return nil, fmt.Errorf("mas-base-url and mas-profile-id are required when --delete is set")
	}
	if cfg.Token == "" {
		if cfg.APITokenName == "" || cfg.APITokenValue == "" {
			return nil, fmt.Errorf("mas-token or mas-api-token-name/value are required when --delete is set")
		}
		token, err := mas.FetchToken(cfg.BaseURL, cfg.APITokenName, cfg.APITokenValue, cfg.InsecureSkipVerify)
		if err != nil {
			return nil, fmt.Errorf("obtain MAS token: %w", err)
		}
		cfg.Token = token
		if cfg.AuthType == "" || cfg.AuthType == "api-key" {
			cfg.AuthType = "jwt"
		}
	}
	return mas.NewClient(cfg)
}
