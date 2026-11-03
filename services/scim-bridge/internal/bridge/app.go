package bridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/config"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/health"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/keycloak"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/mas"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/state"
)

// App wires together configuration, clients, and orchestration logic.
type App struct {
	cfg    config.Settings
	logger *slog.Logger
	// cycleRetryBase is the first wait after a failed poll cycle; it doubles
	// per consecutive failure, capped at the poll interval.
	cycleRetryBase time.Duration
}

// NewApp constructs the runtime application container.
func NewApp(cfg config.Settings) *App {
	var level slog.Level
	switch strings.ToLower(cfg.Bridge.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	return &App{cfg: cfg, logger: logger, cycleRetryBase: 10 * time.Second}
}

// Run performs setup and kicks off the requested sync loop.
//
// Only configuration problems detected at startup (bad URLs, unreadable CA
// files, unreadable state, health port in use) return an error. In
// poll/hybrid mode every runtime failure (Keycloak or MAS down, token mint
// failing) is logged and retried with backoff; Run returns nil when ctx is
// cancelled.
func (a *App) Run(ctx context.Context) error {
	kc, err := keycloak.NewClient(a.cfg.Keycloak)
	if err != nil {
		return fmt.Errorf("init keycloak client: %w", err)
	}

	// Built before any token exists so config errors (URL, CA file) surface
	// at startup; the token is minted per cycle in prepareMAS.
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
	if a.cfg.Bridge.DryRun {
		// Dry-run reads real state but must never write it: a fabricated
		// correlation would make a later live run skip the real create.
		store = state.ReadOnly(store)
	}
	fingerprint := ConfigFingerprint(a.cfg)

	a.logger.Info("SCIM bridge configuration",
		"mode", a.cfg.Bridge.Mode,
		"poll_interval", a.cfg.Bridge.PollInterval.String(),
		"state_backend", a.cfg.Bridge.StateBackend,
		"dry_run", a.cfg.Bridge.DryRun,
		"allow_updates", a.cfg.Bridge.AllowUpdates,
		"log_level", a.cfg.Bridge.LogLevel,
		"payload_logging", a.cfg.Bridge.PayloadLogging,
		"keycloak_realm", kc.Realm(),
		"mas_profile_default", resolver.DefaultProfileID(),
		"config_fingerprint", fingerprint,
		"health_port", a.cfg.Bridge.HealthPort,
	)

	planner := NewPlanner(store, resolver, a.logger)
	executor := NewExecutor(masClient, store, a.cfg.Bridge.DryRun, a.cfg.Bridge.AllowUpdates, a.logger).WithConfigFingerprint(fingerprint)
	poller := NewPoller(kc, planner, executor, a.cfg.Bridge.IncludeUsernames, a.cfg.Bridge.IncludeUsernamePrefix, a.cfg.Bridge.IncludeGroups, a.logger)
	switch a.cfg.Bridge.Mode {
	case "poll", "hybrid":
		tracker := health.NewTracker(time.Duration(a.cfg.Bridge.HealthStaleCycles) * a.cfg.Bridge.PollInterval)
		if err := a.startHealthServer(ctx, tracker); err != nil {
			return err
		}
		cycle := func(ctx context.Context) error {
			return a.runWithMASRefresh(ctx, executor, func() error { return poller.RunOnce(ctx) })
		}
		a.runLoop(ctx, a.cfg.Bridge.PollInterval, cycle, func(err error) {
			tracker.RecordCycle(err)
			tracker.SetEntryCounts(countEntries(ctx, store))
		})
		return nil
	case "run-once":
		return a.runWithMASRefresh(ctx, executor, func() error { return poller.RunOnce(ctx) })
	case "backfill":
		backfill := NewBackfill(kc, resolver, store, a.cfg.Bridge.IncludeUsernames, a.cfg.Bridge.IncludeUsernamePrefix, a.cfg.Bridge.IncludeGroups, a.logger)
		return a.runWithMASRefresh(ctx, executor, func() error {
			backfill.WithMASClient(executor.mas)
			return backfill.Run(ctx)
		})
	default:
		return fmt.Errorf("unsupported bridge mode %s", a.cfg.Bridge.Mode)
	}
}

func (a *App) startHealthServer(ctx context.Context, tracker *health.Tracker) error {
	if a.cfg.Bridge.HealthPort == 0 {
		return nil
	}
	addr := net.JoinHostPort("", strconv.Itoa(a.cfg.Bridge.HealthPort))
	ln, err := health.Listen(addr)
	if err != nil {
		return fmt.Errorf("health server: %w", err)
	}
	go func() {
		if err := health.Serve(ctx, ln, tracker.Handler()); err != nil {
			a.logger.Error("health server stopped", "error", err)
		}
	}()
	a.logger.Info("health endpoints listening", "addr", addr, "paths", "/healthz,/readyz,/status")
	return nil
}

// runLoop runs cycle immediately and then every interval until ctx is
// cancelled. A failed cycle (Keycloak restarting, MAS down) is logged and
// retried after a backoff that starts at cycleRetryBase and doubles up to
// the poll interval; it never ends the process.
func (a *App) runLoop(ctx context.Context, interval time.Duration, cycle func(context.Context) error, observe func(error)) {
	failures := 0
	for {
		err := cycle(ctx)
		if ctx.Err() != nil {
			return
		}
		if observe != nil {
			observe(err)
		}
		wait := interval
		if err != nil {
			failures++
			wait = a.cycleRetryBase
			for i := 1; i < failures && wait < interval; i++ {
				wait *= 2
			}
			if wait > interval {
				wait = interval
			}
			a.logger.Error("poll cycle failed; will retry", "error", err, "consecutive_failures", failures, "retry_in", wait.String())
		} else {
			if failures > 0 {
				a.logger.Info("poll cycle recovered", "after_failures", failures)
			}
			failures = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// countEntries summarises state for /status.
func countEntries(ctx context.Context, store state.Store) (tracked, errored, parked int) {
	entries, err := store.List(ctx)
	if err != nil {
		return 0, 0, 0
	}
	for _, e := range entries {
		tracked++
		if e.Status == state.StatusError {
			errored++
			if e.Parked {
				parked++
			}
		}
	}
	return tracked, errored, parked
}

// runWithMASRefresh mints a MAS token if none is held yet, runs one cycle,
// and on a MAS 401 refreshes the token and retries the cycle once.
func (a *App) runWithMASRefresh(ctx context.Context, executor *Executor, run func() error) error {
	if err := a.prepareMAS(ctx, executor); err != nil {
		return err
	}
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
	if err := a.ensureMASToken(ctx, true); err != nil {
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

// prepareMAS mints the first MAS token lazily, inside the retried cycle, so
// MAS being unreachable at startup is a retryable cycle failure rather
// than a crash.
func (a *App) prepareMAS(ctx context.Context, executor *Executor) error {
	if a.cfg.MAS.Token != "" {
		return nil
	}
	if err := a.ensureMASToken(ctx, false); err != nil {
		return err
	}
	masClient, err := a.newMASClient()
	if err != nil {
		return fmt.Errorf("rebuild MAS client after token fetch: %w", err)
	}
	executor.SetMASClient(masClient)
	return nil
}

func (a *App) canRefreshMASToken() bool {
	return a.cfg.MAS.APITokenName != "" && a.cfg.MAS.APITokenValue != ""
}

func (a *App) ensureMASToken(ctx context.Context, force bool) error {
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
	token, err := mas.FetchToken(ctx, a.cfg.MAS, a.cfg.MAS.APITokenName, a.cfg.MAS.APITokenValue)
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
	cfg := a.cfg.MAS
	cfg.PayloadLogging = a.cfg.Bridge.PayloadLogging
	return mas.NewClient(cfg)
}
