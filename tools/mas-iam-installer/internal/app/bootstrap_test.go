package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalLauncherIsValidBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not found")
	}
	path := filepath.Join(t.TempDir(), "mas-est")
	if err := os.WriteFile(path, []byte(renderLocalLauncher(".mas-est-runtime")), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bash, "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("bash -n on the rendered launcher: %v\n%s", err, out)
	}
}

func TestLocalLauncherPointsWindowsShellsAtWSL(t *testing.T) {
	launcher := renderLocalLauncher(".mas-est-runtime")
	for _, want := range []string{"MINGW*|MSYS*|CYGWIN*)", "WSL2"} {
		if !strings.Contains(launcher, want) {
			t.Errorf("launcher is missing %q", want)
		}
	}
}
