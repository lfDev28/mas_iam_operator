package app

import (
	"context"
	"fmt"
	"os"
	"time"

	executil "github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/exec"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/ui"
)

type clusterContext struct {
	User   string
	Server string
}

func ensureClusterLogin(ctx context.Context, runner *executil.Runner, client *oc.Client, interactive bool) (clusterContext, error) {
	if err := client.CheckAvailable(); err != nil {
		return clusterContext{}, err
	}

	cluster, err := currentClusterContext(ctx, client)
	if err == nil {
		return cluster, nil
	}
	if !interactive {
		return clusterContext{}, fmt.Errorf("%w. Run 'oc login' first, then rerun this command", err)
	}

	fmt.Fprintf(os.Stdout, "[preflight] no active OpenShift login detected: %v\n", err)
	confirm, confirmErr := ui.Confirm("Run oc login now?", true)
	if confirmErr != nil {
		return clusterContext{}, confirmErr
	}
	if !confirm {
		return clusterContext{}, fmt.Errorf("OpenShift login is required; run 'oc login' first, then rerun this command")
	}

	if err := runner.Interactive(ctx, executil.Options{Name: "oc", Args: []string{"login"}}, os.Stdin, os.Stdout, os.Stderr); err != nil {
		return clusterContext{}, err
	}

	cluster, err = currentClusterContext(ctx, client)
	if err != nil {
		return clusterContext{}, fmt.Errorf("OpenShift login still could not be verified: %w", err)
	}
	return cluster, nil
}

func currentClusterContext(ctx context.Context, client *oc.Client) (clusterContext, error) {
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	user, server, err := client.WhoAmI(checkCtx)
	if err != nil {
		return clusterContext{}, err
	}
	return clusterContext{User: user, Server: server}, nil
}
