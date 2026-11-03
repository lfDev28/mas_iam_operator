package bridge

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/config"
)

func testSettings(keycloakURL, masURL string) config.Settings {
	cfg := config.DefaultSettings()
	cfg.Keycloak.BaseURL = keycloakURL
	cfg.Keycloak.Realm = "test"
	cfg.Keycloak.ClientID = "cid"
	cfg.Keycloak.ClientSecret = "secret"
	cfg.MAS.BaseURL = masURL + "/scim/v2"
	cfg.MAS.ProfileID = "p"
	cfg.Bridge.PollInterval = 20 * time.Millisecond
	cfg.Bridge.HealthPort = 0
	cfg.Bridge.LogLevel = "error"
	return cfg
}

func TestRunLoopSurvivesCycleErrorsAndRecovers(t *testing.T) {
	app := NewApp(config.Settings{Bridge: config.BridgeConfig{LogLevel: "error"}})
	app.cycleRetryBase = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var outcomes []error
	calls := 0
	cycle := func(context.Context) error {
		calls++
		if calls <= 3 {
			return errors.New("keycloak: connection refused")
		}
		return nil
	}
	done := make(chan struct{})
	go func() {
		app.runLoop(ctx, 5*time.Millisecond, cycle, func(err error) {
			mu.Lock()
			outcomes = append(outcomes, err)
			n := len(outcomes)
			mu.Unlock()
			if n == 5 {
				cancel()
			}
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("loop did not stop on cancel")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(outcomes) != 5 || outcomes[2] == nil || outcomes[3] != nil || outcomes[4] != nil {
		t.Fatalf("expected 3 failures then successes, got %v", outcomes)
	}
}

// Keycloak returning errors (e.g. restarting) must not end the process.
func TestRunPollModeKeepsRunningWhileKeycloakIsDown(t *testing.T) {
	var kcCalls atomic.Int32
	kc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kcCalls.Add(1)
		http.Error(w, "starting", http.StatusServiceUnavailable)
	}))
	defer kc.Close()
	masSrv := httptest.NewServer(http.NotFoundHandler())
	defer masSrv.Close()

	cfg := testSettings(kc.URL, masSrv.URL)
	cfg.MAS.Token = "static-token"
	app := NewApp(cfg)
	app.cycleRetryBase = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- app.Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for kcCalls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case err := <-errCh:
		t.Fatalf("Run exited while Keycloak was down: %v", err)
	default:
	}
	if kcCalls.Load() < 3 {
		t.Fatalf("expected repeated cycles against a failing Keycloak, got %d calls", kcCalls.Load())
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run should return nil on cancel, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Run did not return after cancel")
	}
}

// MAS unreachable at startup (token mint fails) is a retryable cycle
// failure, not a crash.
func TestRunPollModeSurvivesTokenMintFailureAtStartup(t *testing.T) {
	var authCalls atomic.Int32
	masSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authCalls.Add(1)
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer masSrv.Close()
	cfg := testSettings("http://127.0.0.1:1", masSrv.URL)
	cfg.MAS.APITokenName = "name"
	cfg.MAS.APITokenValue = "value"
	app := NewApp(cfg)
	app.cycleRetryBase = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- app.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for authCalls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case err := <-errCh:
		t.Fatalf("Run exited on token mint failure: %v", err)
	default:
	}
	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("expected nil on cancel, got %v", err)
	}
}

func TestRunFailsFastOnStartupConfigError(t *testing.T) {
	cfg := testSettings("https://keycloak.example", "https://mas.example")
	cfg.MAS.Token = "t"
	cfg.Keycloak.CAFile = "/nonexistent/keycloak-ca.crt"
	if err := NewApp(cfg).Run(context.Background()); err == nil {
		t.Fatalf("expected startup error for unreadable CA file")
	}
}
