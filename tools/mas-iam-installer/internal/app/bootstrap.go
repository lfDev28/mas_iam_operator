package app

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const (
	defaultBootstrapOutputDir = "/tmp"
	defaultBootstrapCommand   = "mas-est"
	bundledRuntimeRoot        = "/opt/mas-est/runtime"
)

type bootstrapOptions struct {
	outputDir   string
	commandName string
	force       bool
}

func newBootstrapCommand() *cobra.Command {
	opts := &bootstrapOptions{
		outputDir:   defaultBootstrapOutputDir,
		commandName: defaultBootstrapCommand,
	}

	command := &cobra.Command{
		Use:   "bootstrap",
		Short: "Install the local mas-est runtime into a mounted host directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.run(cmd.OutOrStdout())
		},
	}

	flags := command.Flags()
	flags.StringVar(&opts.outputDir, "output-dir", opts.outputDir, "Mounted host directory where the local mas-est runtime should be written")
	flags.StringVar(&opts.commandName, "command-name", opts.commandName, "Name of the installed host command")
	flags.BoolVar(&opts.force, "force", false, "Overwrite an existing local runtime")

	return command
}

func (o *bootstrapOptions) run(output io.Writer) error {
	outputDir := strings.TrimSpace(o.outputDir)
	if outputDir == "" {
		return fmt.Errorf("--output-dir must not be empty")
	}

	commandName := filepath.Base(strings.TrimSpace(o.commandName))
	if commandName == "." || commandName == string(filepath.Separator) || commandName == "" {
		return fmt.Errorf("--command-name must be a valid file name")
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir %s: %w", outputDir, err)
	}

	runtimeDirName := runtimeDirectoryName(commandName)
	runtimeDir := filepath.Join(outputDir, runtimeDirName)
	wrapperPath := filepath.Join(outputDir, commandName)

	if !o.force {
		if _, err := os.Stat(wrapperPath); err == nil {
			return fmt.Errorf("%s already exists; rerun with --force to overwrite", wrapperPath)
		}
		if _, err := os.Stat(runtimeDir); err == nil {
			return fmt.Errorf("%s already exists; rerun with --force to overwrite", runtimeDir)
		}
	}

	stagingDir, err := os.MkdirTemp(outputDir, runtimeDirName+".staging-")
	if err != nil {
		return fmt.Errorf("create staging directory in %s: %w", outputDir, err)
	}
	defer os.RemoveAll(stagingDir)

	if err := populateRuntimeTree(stagingDir); err != nil {
		return err
	}

	wrapperTempPath := wrapperPath + ".tmp"
	if err := os.WriteFile(wrapperTempPath, []byte(renderLocalLauncher(runtimeDirName)), 0o755); err != nil {
		return fmt.Errorf("write launcher %s: %w", wrapperTempPath, err)
	}
	defer os.Remove(wrapperTempPath)

	if o.force {
		if err := os.RemoveAll(runtimeDir); err != nil {
			return fmt.Errorf("remove existing runtime %s: %w", runtimeDir, err)
		}
		if err := os.Remove(wrapperPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove existing launcher %s: %w", wrapperPath, err)
		}
	}

	if err := os.Rename(stagingDir, runtimeDir); err != nil {
		return fmt.Errorf("install runtime to %s: %w", runtimeDir, err)
	}
	if err := os.Rename(wrapperTempPath, wrapperPath); err != nil {
		return fmt.Errorf("install launcher to %s: %w", wrapperPath, err)
	}

	fmt.Fprintf(output, "[result] installed local runtime at container path: %s\n", runtimeDir)
	fmt.Fprintf(output, "[result] installed launcher at container path: %s\n", wrapperPath)
	fmt.Fprintf(output, "[result] add the corresponding host directory for %s to PATH\n", outputDir)
	fmt.Fprintln(output, "[result] host requirements for install/uninstall: bash 3.2+, oc")
	fmt.Fprintf(output, "[result] then run: %s install\n", commandName)
	return nil
}

func populateRuntimeTree(destination string) error {
	layout := map[string]string{
		filepath.Join(destination, "repo", "scripts"):   "/opt/mas-est/scripts",
		filepath.Join(destination, "repo", "manifests"): "/opt/mas-est/manifests",
		filepath.Join(destination, "repo", "env"):       "/opt/mas-est/env",
		filepath.Join(destination, "bin"):               filepath.Join(bundledRuntimeRoot, "bin"),
	}

	for target, source := range layout {
		if err := copyTree(source, target); err != nil {
			return fmt.Errorf("copy %s to %s: %w", source, target, err)
		}
	}

	return nil
}

func copyTree(source, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", source)
	}

	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(destination, relative)

		info, err := entry.Info()
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return os.MkdirAll(targetPath, info.Mode().Perm())
		}

		return copyFile(path, targetPath, info.Mode().Perm())
	})
}

func copyFile(source, destination string, mode fs.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}

	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer output.Close()

	if _, err := io.Copy(output, input); err != nil {
		return err
	}

	return nil
}

func runtimeDirectoryName(commandName string) string {
	var builder strings.Builder
	builder.WriteByte('.')
	for _, r := range commandName {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('-')
		}
	}
	builder.WriteString("-runtime")
	return builder.String()
}

func renderLocalLauncher(runtimeDirName string) string {
	return fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RUNTIME_DIR="${MAS_EST_RUNTIME_DIR:-${SELF_DIR}/%s}"
REPO_ROOT="${MAS_EST_REPO_ROOT:-${MAS_IAM_REPO_ROOT:-${RUNTIME_DIR}/repo}}"
requested_command="${1:-}"

os_name="$(uname -s)"
arch_name="$(uname -m)"

case "${os_name}" in
  Darwin) os_name="darwin" ;;
  Linux) os_name="linux" ;;
  *)
    echo "error: unsupported operating system ${os_name}" >&2
    exit 1
    ;;
esac

case "${arch_name}" in
  x86_64|amd64) arch_name="amd64" ;;
  arm64|aarch64) arch_name="arm64" ;;
  *)
    echo "error: unsupported CPU architecture ${arch_name}" >&2
    exit 1
    ;;
esac

binary_path="${RUNTIME_DIR}/bin/${os_name}-${arch_name}/est"
if [[ ! -x "${binary_path}" ]]; then
  echo "error: runtime binary not found: ${binary_path}" >&2
  exit 1
fi

if [[ ! -d "${REPO_ROOT}/scripts" ]]; then
  echo "error: runtime assets not found under ${REPO_ROOT}" >&2
  exit 1
fi

require_command() {
  local command_name="$1"
  local description="${2:-$1}"
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "error: ${description} is required on PATH" >&2
    exit 1
  fi
}

if [[ "${requested_command}" == "install" || "${requested_command}" == "uninstall" || "${requested_command}" == "wipe" ]]; then
  require_command bash "bash 3.2+"
  bash_major="$(bash -c 'printf "%%s" "${BASH_VERSINFO[0]}"' 2>/dev/null || printf '0')"
  bash_minor="$(bash -c 'printf "%%s" "${BASH_VERSINFO[1]}"' 2>/dev/null || printf '0')"
  if [[ "${bash_major}" -lt 3 ]] || { [[ "${bash_major}" -eq 3 ]] && [[ "${bash_minor}" -lt 2 ]]; }; then
    echo "error: mas-est install/uninstall require bash 3.2+ on the host PATH" >&2
    exit 1
  fi
fi

case "${requested_command}" in
  install|preflight|status|logs|uninstall|wipe)
    require_command oc "oc CLI"
    ;;
esac

exec env MAS_EST_REPO_ROOT="${REPO_ROOT}" MAS_EST_RENDERER_BINARY="${binary_path}" MAS_IAM_REPO_ROOT="${REPO_ROOT}" MAS_IAM_RENDERER_BINARY="${binary_path}" "${binary_path}" "$@"
`, runtimeDirName)
}
