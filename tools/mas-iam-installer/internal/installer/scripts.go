package installer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	executil "github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/exec"
)

type Paths struct {
	RepoRoot      string
	InstallScript string
	WipeScript    string
}

const bundledRepoRoot = "/opt/mas-est"

func DiscoverPaths(override string) (Paths, error) {
	candidates := []string{}
	seen := map[string]struct{}{}

	addCandidate := func(path string) {
		if strings.TrimSpace(path) == "" {
			return
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return
		}
		for current := absolute; ; current = filepath.Dir(current) {
			if _, ok := seen[current]; !ok {
				seen[current] = struct{}{}
				candidates = append(candidates, current)
			}
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
		}
	}

	addCandidate(override)
	addCandidate(os.Getenv(config.EnvRepoRoot))
	addCandidate(os.Getenv(config.LegacyEnvRepoRoot))
	addCandidate(bundledRepoRoot)

	if cwd, err := os.Getwd(); err == nil {
		addCandidate(cwd)
	}
	if executable, err := os.Executable(); err == nil {
		addCandidate(filepath.Dir(executable))
	}

	for _, root := range candidates {
		installScript := filepath.Join(root, "scripts", "install-all-in-one.sh")
		wipeScript := filepath.Join(root, "scripts", "wipe-all-in-one.sh")
		if fileExists(installScript) && fileExists(wipeScript) {
			return Paths{
				RepoRoot:      root,
				InstallScript: installScript,
				WipeScript:    wipeScript,
			}, nil
		}
	}

	return Paths{}, fmt.Errorf("could not find repo root containing scripts/install-all-in-one.sh and scripts/wipe-all-in-one.sh")
}

func RunInstall(ctx context.Context, runner *executil.Runner, paths Paths, cfg config.InstallConfig, output io.Writer) error {
	args := []string{paths.InstallScript, "--namespace", cfg.Namespace, "--keycloak-bootstrap", cfg.KeycloakBootstrapMethod}
	if cfg.StorageClass != "" {
		args = append(args, "--storage-class", cfg.StorageClass)
	}
	env := cfg.ScriptEnv()
	addRendererBinaryEnv(env)

	return runner.Stream(ctx, executil.Options{
		Name: "bash",
		Args: args,
		Dir:  paths.RepoRoot,
		Env:  env,
	}, output, output)
}

func RunWipe(ctx context.Context, runner *executil.Runner, paths Paths, cfg config.WipeConfig, output io.Writer) error {
	args := []string{paths.WipeScript, "--namespace", cfg.Namespace, "--profile-id", cfg.ProfileID}
	if cfg.SkipProfileDelete {
		args = append(args, "--skip-profile-delete")
	}
	env := cfg.ScriptEnv()
	addRendererBinaryEnv(env)

	return runner.Stream(ctx, executil.Options{
		Name: "bash",
		Args: args,
		Dir:  paths.RepoRoot,
		Env:  env,
	}, output, output)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func addRendererBinaryEnv(env map[string]string) {
	executable, err := os.Executable()
	if err != nil || strings.TrimSpace(executable) == "" {
		return
	}
	env[config.EnvRendererBinary] = executable
	env[config.LegacyEnvRendererBinary] = executable
}
