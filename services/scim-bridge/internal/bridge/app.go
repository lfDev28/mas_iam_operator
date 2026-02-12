package bridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/config"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/keycloak"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/mas"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/state"
)

// App wires together configuration, clients, and orchestration logic.
type App struct {
	cfg    config.Settings
	logger *slog.Logger
}

// NewApp constructs the runtime application container.
func NewApp(cfg config.Settings) *App {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))
	return &App{cfg: cfg, logger: logger}
}

// Run performs setup and kicks off the requested sync loop.
func (a *App) Run(ctx context.Context) error {
	if err := a.ensureMASToken(false); err != nil {
		return err
	}
	kc, err := keycloak.NewClient(a.cfg.Keycloak)
	if err != nil {
		return fmt.Errorf("init keycloak client: %w", err)
	}

	masClient, err := a.newMASClient()
	if err != nil {
		return fmt.Errorf("init MAS client: %w", err)
	}

	resolver := NewProfileResolver(a.cfg.MAS.ProfileID, a.cfg.MAS.ProfileMap, a.cfg.MAS.ProfileRequireLabel)

	store, err := state.NewStore(a.cfg.Bridge.StateBackend, state.Options{
		FilePath:         a.cfg.Bridge.StatePath,
		DefaultProfileID: resolver.DefaultProfileID(),
	})
	if err != nil {
		return fmt.Errorf("init state backend: %w", err)
	}
	defer store.Close(ctx)

	a.logger.Info("SCIM bridge configuration",
		"mode", a.cfg.Bridge.Mode,
		"poll_interval", a.cfg.Bridge.PollInterval.String(),
		"state_backend", a.cfg.Bridge.StateBackend,
		"allow_updates", a.cfg.Bridge.AllowUpdates,
		"keycloak_realm", kc.Realm(),
		"mas_profile_default", resolver.DefaultProfileID(),
	)

	planner := NewPlanner(store, resolver, a.logger)
	executor := NewExecutor(masClient, store, a.cfg.Bridge.DryRun, a.cfg.Bridge.AllowUpdates, a.logger)
	poller := NewPoller(kc, planner, executor, a.cfg.Bridge.IncludeUsernames, a.cfg.Bridge.IncludeUsernamePrefix, a.logger)
	switch a.cfg.Bridge.Mode {
	case "poll", "hybrid":
		return a.runPollingLoop(ctx, poller, executor)
	case "event":
		a.logger.Warn("event mode not yet implemented; exiting")
		return nil
	case "run-once":
		return a.runWithMASRefresh(ctx, executor, func() error { return poller.RunOnce(ctx) })
	case "backfill":
		backfill := NewBackfill(kc, resolver, store, a.cfg.Bridge.IncludeUsernames, a.cfg.Bridge.IncludeUsernamePrefix, a.logger).WithMASClient(masClient)
		return a.runWithMASRefresh(ctx, executor, func() error {
			backfill.WithMASClient(executor.mas)
			return backfill.Run(ctx)
		})
	default:
		return fmt.Errorf("unsupported bridge mode %s", a.cfg.Bridge.Mode)
	}
}

func (a *App) runPollingLoop(ctx context.Context, poller *Poller, executor *Executor) error {
	if err := a.runWithMASRefresh(ctx, executor, func() error { return poller.RunOnce(ctx) }); err != nil {
		return err
	}
	ticker := time.NewTicker(a.cfg.Bridge.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := a.runWithMASRefresh(ctx, executor, func() error { return poller.RunOnce(ctx) }); err != nil {
				a.logger.Error("poll iteration failed", "error", err)
				return err
			}
		}
	}
}

func (a *App) runWithMASRefresh(ctx context.Context, executor *Executor, run func() error) error {
	err := run()
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrMASUnauthorized) {
		return err
	}
	if !a.canRefreshMASToken() {
		return err
	}
	a.logger.Warn("MAS returned unauthorized; refreshing token and retrying once")
	if err := a.ensureMASToken(true); err != nil {
		return fmt.Errorf("refresh MAS token after unauthorized: %w", err)
	}
	masClient, err := a.newMASClient()
	if err != nil {
		return fmt.Errorf("rebuild MAS client after refresh: %w", err)
	}
	executor.SetMASClient(masClient)
	if err := run(); err != nil {
		return fmt.Errorf("retry after MAS token refresh: %w", err)
	}
	return nil
}

func (a *App) canRefreshMASToken() bool {
	return a.cfg.MAS.APITokenName != "" && a.cfg.MAS.APITokenValue != ""
}

func (a *App) ensureMASToken(force bool) error {
	if !force && a.cfg.MAS.Token != "" {
		return nil
	}
	if !a.canRefreshMASToken() {
		if force {
			return fmt.Errorf("cannot refresh MAS token: SCIM_BRIDGE_MAS_API_TOKEN_NAME/VALUE are not set")
		}
		if a.cfg.MAS.Token == "" {
			return fmt.Errorf("obtain MAS token: missing SCIM_BRIDGE_MAS_TOKEN and SCIM_BRIDGE_MAS_API_TOKEN_NAME/VALUE")
		}
		return nil
	}
	token, err := mas.FetchToken(a.cfg.MAS.BaseURL, a.cfg.MAS.APITokenName, a.cfg.MAS.APITokenValue, a.cfg.MAS.InsecureSkipVerify)
	if err != nil {
		return fmt.Errorf("obtain MAS token: %w", err)
	}
	a.cfg.MAS.Token = token
	if a.cfg.MAS.AuthType == "" || strings.ToLower(a.cfg.MAS.AuthType) == "api-key" {
		a.cfg.MAS.AuthType = "jwt"
	}
	if force {
		a.logger.Info("MAS token refreshed")
	}
	return nil
}

func (a *App) newMASClient() (*mas.Client, error) {
	return mas.NewClient(a.cfg.MAS)
}
