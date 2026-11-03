package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/config"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/mas"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/state"
)

const usage = `scim-bridge-cleanup inspects and repairs the SCIM bridge state file.

Stop the bridge before changing state (it rewrites state.json from memory):
  oc scale deploy/scim-bridge -n mas-est --replicas=0

Modes:
  (no mode flag)   List status=error and parked entries. Changes nothing.

  --reset-state    RECOMMENDED RECOVERY. Clears error/parked bookkeeping in
                   state without touching MAS: entries with no MAS user (failed
                   or dry-run creates) are dropped so the bridge creates them;
                   entries with a MAS user are queued for an immediate retry.
                   Use after fixing the cause (MAS profile, config, a
                   conflicting MAS user).

  --delete         Deletes MAS users, then drops their state entries. Only
                   touches parked entries whose MAS user this bridge created
                   (createdByBridge=true). Entries that are merely retrying,
                   have no MAS ID, or point at MAS users the bridge adopted
                   or did not create are skipped. Irreversible.

Flags:
`

type errorEntry struct {
	KeycloakID string
	State      state.Entry
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	cfg := config.DefaultSettings()
	if err := config.ApplyEnvOverrides(&cfg); err != nil {
		return err
	}

	fs := flag.NewFlagSet("scim-bridge-cleanup", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usage)
		fs.PrintDefaults()
	}

	fs.StringVar(&cfg.MAS.BaseURL, "mas-base-url", cfg.MAS.BaseURL, "MAS SCIM base URL (https://api.<mas>/scim/v2); --delete only")
	fs.StringVar(&cfg.MAS.AuthType, "mas-auth-type", cfg.MAS.AuthType, "MAS auth type: api-key or jwt; --delete only")
	fs.StringVar(&cfg.MAS.Token, "mas-token", cfg.MAS.Token, "MAS API key or JWT; --delete only")
	fs.StringVar(&cfg.MAS.APITokenName, "mas-api-token-name", cfg.MAS.APITokenName, "MAS basic auth username for /v1/authenticate (optional if mas-token set)")
	fs.StringVar(&cfg.MAS.APITokenValue, "mas-api-token-value", cfg.MAS.APITokenValue, "MAS basic auth password for /v1/authenticate (optional if mas-token set)")
	fs.StringVar(&cfg.MAS.CAFile, "mas-ca-file", cfg.MAS.CAFile, "Extra CA bundle for MAS TLS (added to system roots)")
	fs.BoolVar(&cfg.MAS.InsecureSkipVerify, "mas-insecure-skip-verify", cfg.MAS.InsecureSkipVerify, "Skip TLS verification for MAS API calls (dev only)")
	fs.StringVar(&cfg.Bridge.StatePath, "state-path", cfg.Bridge.StatePath, "Path to correlation state file (filesystem backend)")
	fs.StringVar(&cfg.MAS.ProfileID, "mas-profile-id", cfg.MAS.ProfileID, "Default MAS SCIM profile ID (used for legacy state entries)")

	resetFlag := fs.Bool("reset-state", false, "Clear error/parked entries in state without touching MAS (recommended recovery)")
	deleteFlag := fs.Bool("delete", false, "Delete bridge-created MAS users for parked entries and remove them from state")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *resetFlag && *deleteFlag {
		return fmt.Errorf("--reset-state and --delete are mutually exclusive")
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
		fmt.Printf("- kc_id=%s username=%s mas_id=%s profile=%s class=%s attempts=%d parked=%t created_by_bridge=%t last_error=%s\n",
			e.KeycloakID, e.State.Username, e.State.MASID, e.State.ProfileID, e.State.ErrorClass, e.State.Attempts, e.State.Parked, e.State.CreatedByBridge, e.State.LastError)
	}

	switch {
	case *resetFlag:
		dropped, requeued := resetState(entries)
		if err := state.WriteToFile(cfg.Bridge.StatePath, entries, cfg.MAS.ProfileID); err != nil {
			return fmt.Errorf("write state: %w", err)
		}
		fmt.Printf("reset complete: %d entries dropped (will be created), %d entries queued for immediate retry; MAS untouched\n", dropped, requeued)
		return nil
	case *deleteFlag:
		return deleteBridgeUsers(cfg, entries, errorEntries)
	default:
		fmt.Println("no changes made. Recommended: --reset-state (fixes state only). --delete removes bridge-created MAS users for parked entries.")
		return nil
	}
}

// resetState clears failure bookkeeping in place. Entries without a real
// MAS user are dropped (the bridge will create them); entries with one keep
// their correlation and snapshot and are retried on the next cycle.
func resetState(entries map[string]state.Entry) (dropped, requeued int) {
	for id, e := range entries {
		if e.Status != state.StatusError {
			continue
		}
		if !e.HasMASUser() {
			delete(entries, id)
			dropped++
			continue
		}
		e.ClearRetry()
		entries[id] = e
		requeued++
	}
	// Stale dry-run correlations from pre-v0.1.16 builds never existed in
	// MAS; drop them whatever their status.
	for id, e := range entries {
		if e.MASID != "" && !e.HasMASUser() {
			delete(entries, id)
			dropped++
		}
	}
	return dropped, requeued
}

// deletionDecision says whether --delete may remove an entry's MAS user.
func deletionDecision(e state.Entry) (ok bool, reason string) {
	switch {
	case e.Status != state.StatusError:
		return false, "not in error"
	case !e.HasMASUser():
		return false, "no MAS user (empty or dry-run ID)"
	case !e.CreatedByBridge:
		return false, "MAS user not created by this bridge"
	case !e.Parked:
		return false, "still retrying (not parked); likely transient"
	default:
		return true, ""
	}
}

func deleteBridgeUsers(cfg config.Settings, entries map[string]state.Entry, errorEntries []errorEntry) error {
	var targets []errorEntry
	for _, e := range errorEntries {
		if ok, reason := deletionDecision(e.State); !ok {
			fmt.Printf("skip kc_id=%s username=%s mas_id=%s: %s\n", e.KeycloakID, e.State.Username, e.State.MASID, reason)
			continue
		}
		targets = append(targets, e)
	}
	if len(targets) == 0 {
		fmt.Println("nothing eligible for deletion")
		return nil
	}

	masClient, err := buildMASClient(cfg.MAS)
	if err != nil {
		return err
	}

	ctx := context.Background()
	deleted := 0
	for _, e := range targets {
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
	sort.Slice(list, func(i, j int) bool { return list[i].KeycloakID < list[j].KeycloakID })
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
		token, err := mas.FetchToken(context.Background(), cfg, cfg.APITokenName, cfg.APITokenValue)
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
