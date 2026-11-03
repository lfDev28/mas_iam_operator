package app

import (
	"strings"
	"testing"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
)

func TestCheckWorkspaceExists(t *testing.T) {
	workspaces := []oc.ManageWorkspace{
		{Name: "mas-dev-ws1", Namespace: "mas-mas-dev-manage", InstanceID: "mas-dev", WorkspaceID: "ws1"},
		{Name: "prod-main", Namespace: "mas-prod-manage", InstanceID: "prod", WorkspaceID: "main"},
	}

	if err := checkWorkspaceExists("ws1", "mas-dev", workspaces); err != nil {
		t.Fatalf("existing workspace rejected: %v", err)
	}
	if err := checkWorkspaceExists("", "mas-dev", workspaces); err != nil {
		t.Fatalf("empty workspace id must be left to Validate: %v", err)
	}
	if err := checkWorkspaceExists("anything", "mas-dev", nil); err != nil {
		t.Fatalf("nothing to verify against must not fail: %v", err)
	}

	err := checkWorkspaceExists("dev-ws1", "mas-dev", workspaces)
	if err == nil {
		t.Fatal("typo'd workspace id accepted")
	}
	if !strings.Contains(err.Error(), `"dev-ws1"`) || !strings.Contains(err.Error(), "ws1 (instance mas-dev") {
		t.Fatalf("error should name the bad id and list the available ones: %v", err)
	}
	if strings.Contains(err.Error(), "main") {
		t.Fatalf("error should only list workspaces of the target instance: %v", err)
	}

	// A workspace of a different instance does not satisfy the check.
	if err := checkWorkspaceExists("main", "mas-dev", workspaces); err == nil {
		t.Fatal("workspace of another instance accepted")
	}
	// Unknown instance: any workspace on the cluster counts.
	if err := checkWorkspaceExists("main", "", workspaces); err != nil {
		t.Fatalf("unscoped check rejected an existing workspace: %v", err)
	}
}

func TestWorkspaceInstanceID(t *testing.T) {
	routes := []oc.MASRoute{{InstanceID: "mas-dev", Host: "api.mas-dev.apps.example.com"}}

	cfg := config.DefaultInstallConfig()
	cfg.ConfigureMASAuth = true
	cfg.MASAuthInstanceID = "authinst"
	if got := workspaceInstanceID(cfg, routes); got != "authinst" {
		t.Fatalf("got %q, want the MAS auth instance", got)
	}

	cfg = config.DefaultInstallConfig()
	cfg.MASBaseURL = "https://api.mas-dev.apps.example.com/scim/v2"
	if got := workspaceInstanceID(cfg, routes); got != "mas-dev" {
		t.Fatalf("got %q, want the instance owning the SCIM route", got)
	}
}
