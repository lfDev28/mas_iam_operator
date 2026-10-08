package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/version"
)

// `mas-est update` replaces the local runtime with the newest release on Quay.
// It does exactly what the documented bootstrap line does - `podman run ...
// bootstrap --force` into the directory holding the launcher - but resolves the
// tag itself, so nobody has to know which version is current.
//
// The runtime is replaced underneath the running binary. That is safe on the
// platforms we ship for: the launcher has already exec'd this process, and an
// unlinked executable keeps running until it exits.

const (
	quayAPIBase = "https://quay.io/api/v1"
	// Quay lists tags newest first; a release is always on the first page in
	// practice. The page cap only bounds a pathological repository.
	quayTagPageSize = 100
	quayMaxTagPages = 5
)

var releaseTagPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

type updateOptions struct {
	root       *RootOptions
	repository string
	target     string
	check      bool
	force      bool
	engine     string
	apiBase    string
	httpClient *http.Client
}

func newUpdateCommand(root *RootOptions) *cobra.Command {
	opts := &updateOptions{
		root:       root,
		repository: installerImageRepository,
		apiBase:    quayAPIBase,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}

	command := &cobra.Command{
		Use:   "update",
		Short: "Update the local mas-est runtime to the newest release",
		Long: `Update the local mas-est runtime to the newest release published on Quay.

Runs the same bootstrap as the install instructions (podman or docker), into the
directory that holds this mas-est launcher. Nothing in the cluster changes until
you run mas-est install again.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.run(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	flags := command.Flags()
	flags.StringVar(&opts.target, "version", "", "Install this tag (for example v0.1.13) instead of the newest release; skips the Quay lookup")
	flags.BoolVar(&opts.check, "check", false, "Only report whether a newer release exists")
	flags.BoolVar(&opts.force, "force", false, "Re-install even when already on the target version")
	flags.StringVar(&opts.engine, "engine", "", "Container engine to run bootstrap with (default: podman, else docker)")

	return command
}

func (o *updateOptions) run(ctx context.Context, out, errOut io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	current := "v" + strings.TrimSpace(version.Version)

	target := strings.TrimSpace(o.target)
	if target != "" && !strings.HasPrefix(target, "v") {
		target = "v" + target
	}
	if target == "" {
		latest, err := latestReleaseTag(ctx, o.httpClient, o.apiBase, o.repository)
		if err != nil {
			return fmt.Errorf("look up the newest release: %w\nPass --version vX.Y.Z to skip the lookup", err)
		}
		target = latest
	}

	fmt.Fprintf(out, "[update] installed: %s\n", current)
	fmt.Fprintf(out, "[update] target:    %s\n", target)

	// Without --version, only move forward: a dev build (0.1.14-dev) or a
	// locally built newer binary must not be "updated" back to the release.
	if o.target == "" && !o.force && !releaseNewer(target, current) {
		fmt.Fprintln(out, "[update] already up to date")
		return nil
	}
	if o.target != "" && !o.force && target == current {
		fmt.Fprintln(out, "[update] already on that version; pass --force to re-install")
		return nil
	}
	if o.check {
		fmt.Fprintf(out, "[update] %s is available; run: mas-est update\n", target)
		return nil
	}

	installDir, commandName, err := launcherLocation(o.root.RepoRoot)
	if err != nil {
		return err
	}

	engine, err := resolveContainerEngine(o.engine)
	if err != nil {
		return err
	}

	image := o.repository + ":" + target
	args := []string{
		"run", "--rm", "--pull", "always",
		"-v", installDir + ":/tmp",
		image,
		"bootstrap", "--force", "--command-name", commandName,
	}
	fmt.Fprintf(out, "[update] %s %s\n", engine, strings.Join(args, " "))

	// Bootstrap's own stdout speaks in container paths ("add /tmp to PATH"),
	// which is wrong advice here; show it only when something fails. Pull
	// progress is on stderr and streams through.
	var bootstrapOut bytes.Buffer
	bootstrap := exec.CommandContext(ctx, engine, args...)
	bootstrap.Stdout = &bootstrapOut
	bootstrap.Stderr = errOut
	if err := bootstrap.Run(); err != nil {
		out.Write(bootstrapOut.Bytes())
		return fmt.Errorf("bootstrap from %s failed: %w", image, err)
	}

	fmt.Fprintf(out, "\n[update] mas-est %s installed in %s\n", target, installDir)

	// The stale-runtime guard compares MAS_EST_IMAGE against the binary, so a
	// shell still exporting the old tag would make the next install refuse.
	if requested := strings.TrimSpace(os.Getenv(masESTImageEnv)); requested != "" {
		if tag := imageRefTag(requested); tag != "" && tag != target {
			fmt.Fprintf(out, "[update] your shell still has %s=%s; install refuses to run until you update it:\n\n  export %s='%s'\n",
				masESTImageEnv, requested, masESTImageEnv, image)
		}
	}
	return nil
}

// latestReleaseTag returns the highest vX.Y.Z tag in a quay.io repository.
// Pre-release (-dev, -beta.N) and per-arch (-amd64, -arm64) tags never match.
func latestReleaseTag(ctx context.Context, client *http.Client, apiBase, repository string) (string, error) {
	path, ok := strings.CutPrefix(repository, "quay.io/")
	if !ok {
		return "", fmt.Errorf("%s is not a quay.io repository", repository)
	}

	best := ""
	for page := 1; page <= quayMaxTagPages; page++ {
		endpoint := fmt.Sprintf("%s/repository/%s/tag/?onlyActiveTags=true&limit=%d&page=%d",
			strings.TrimRight(apiBase, "/"), path, quayTagPageSize, page)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return "", err
		}
		response, err := client.Do(request)
		if err != nil {
			return "", err
		}
		var body struct {
			Tags []struct {
				Name string `json:"name"`
			} `json:"tags"`
			HasAdditional bool `json:"has_additional"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&body)
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return "", fmt.Errorf("GET %s: HTTP %d", endpoint, response.StatusCode)
		}
		if decodeErr != nil {
			return "", fmt.Errorf("GET %s: decode: %w", endpoint, decodeErr)
		}

		for _, tag := range body.Tags {
			if releaseTagPattern.MatchString(tag.Name) && (best == "" || releaseNewer(tag.Name, best)) {
				best = tag.Name
			}
		}
		if !body.HasAdditional {
			break
		}
	}

	if best == "" {
		return "", fmt.Errorf("no vX.Y.Z release tag found in %s", repository)
	}
	return best, nil
}

// releaseNewer reports whether release tag a is a higher version than b. b may
// be a pre-release like v0.1.14-dev: its X.Y.Z core is compared, and a release
// with the same core counts as newer.
func releaseNewer(a, b string) bool {
	av, ok := parseVersionCore(a)
	if !ok {
		return false
	}
	bv, ok := parseVersionCore(b)
	if !ok {
		return true
	}
	for i := range av {
		if av[i] != bv[i] {
			return av[i] > bv[i]
		}
	}
	return strings.Contains(b, "-") && !strings.Contains(a, "-")
}

func parseVersionCore(tag string) ([3]int, bool) {
	var parts [3]int
	core, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(tag), "v"), "-")
	fields := strings.Split(core, ".")
	if len(fields) != 3 {
		return parts, false
	}
	for i, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil {
			return parts, false
		}
		parts[i] = n
	}
	return parts, true
}

// launcherLocation finds the bootstrap directory from the repo root the
// launcher exported: <dir>/.<command>-runtime/repo. It refuses anything else,
// such as a binary run from a checkout or inside the installer image.
func launcherLocation(repoRoot string) (string, string, error) {
	notBootstrapped := fmt.Errorf("mas-est update only works from a bootstrapped mas-est (the launcher in ~/mas-est); this binary was started from %q", repoRoot)

	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" || filepath.Base(repoRoot) != "repo" {
		return "", "", notBootstrapped
	}
	runtimeDir := filepath.Dir(repoRoot)
	runtimeName := filepath.Base(runtimeDir)
	if !strings.HasPrefix(runtimeName, ".") || !strings.HasSuffix(runtimeName, "-runtime") {
		return "", "", notBootstrapped
	}

	commandName := strings.TrimSuffix(strings.TrimPrefix(runtimeName, "."), "-runtime")
	installDir, err := filepath.Abs(filepath.Dir(runtimeDir))
	if err != nil {
		return "", "", err
	}
	if _, err := os.Stat(filepath.Join(installDir, commandName)); err != nil {
		return "", "", notBootstrapped
	}
	return installDir, commandName, nil
}

func resolveContainerEngine(requested string) (string, error) {
	if requested = strings.TrimSpace(requested); requested != "" {
		if _, err := exec.LookPath(requested); err != nil {
			return "", fmt.Errorf("container engine %q not found on PATH", requested)
		}
		return requested, nil
	}
	for _, candidate := range []string{"podman", "docker"} {
		if _, err := exec.LookPath(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("mas-est update needs podman or docker on PATH")
}
