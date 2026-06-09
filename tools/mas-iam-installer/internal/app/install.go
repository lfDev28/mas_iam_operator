package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	executil "github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/exec"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/installer"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/logging"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/preflight"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/ui"
)

type installOptions struct {
	config         config.InstallConfig
	nonInteractive bool
	yes            bool
}

type installDiscovery struct {
	MASRoutes  []oc.MASRoute
	Workspaces []oc.ManageWorkspace
}

func newInstallCommand(root *RootOptions) *cobra.Command {
	opts := &installOptions{
		config: config.LoadInstallConfigFromEnv(),
	}

	command := &cobra.Command{
		Use:   "install",
		Short: "Install or reinstall selected MAS EST services",
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.run(cmd.Context(), root)
		},
	}

	flags := command.Flags()
	flags.StringSliceVar(&opts.config.Components, "components", opts.config.Components, "Comma-separated components to install: ldap,keycloak,scim,s3,smtp")
	flags.StringVar(&opts.config.Namespace, "namespace", opts.config.Namespace, "Target namespace")
	flags.StringVar(&opts.config.MASBaseURL, "mas-base-url", opts.config.MASBaseURL, "MAS SCIM base URL, including /scim/v2")
	flags.StringVar(&opts.config.MASAPITokenName, "mas-api-token-name", opts.config.MASAPITokenName, "MAS API token name")
	flags.StringVar(&opts.config.MASAPITokenValue, "mas-api-token-value", opts.config.MASAPITokenValue, "MAS API token value")
	flags.StringVar(&opts.config.WorkspaceID, "workspace-id", opts.config.WorkspaceID, "MAS workspace ID for profile bootstrap")
	flags.StringVar(&opts.config.ProfileID, "profile-id", opts.config.ProfileID, "MAS profile ID")
	flags.StringVar(&opts.config.MASInstanceID, "mas-instance-id", opts.config.MASInstanceID, "MAS instance ID for S3 ObjectStorageCfg")
	flags.StringVar(&opts.config.MASCoreNamespace, "mas-core-namespace", opts.config.MASCoreNamespace, "MAS core namespace for S3 ObjectStorageCfg")
	flags.StringVar(&opts.config.StorageClass, "storage-class", opts.config.StorageClass, "PostgreSQL storage class override")
	flags.StringVar(&opts.config.ScimBridgeStorageClass, "scim-bridge-storage-class", opts.config.ScimBridgeStorageClass, "SCIM bridge PVC storage class override")
	flags.StringVar(&opts.config.KeycloakBootstrapMethod, "keycloak-bootstrap", opts.config.KeycloakBootstrapMethod, "Keycloak bootstrap mode passed to install-all-in-one.sh")
	flags.BoolVar(&opts.config.SkipS3MASConfig, "skip-s3-mas-config", opts.config.SkipS3MASConfig, "Install S3 storage without creating MAS ObjectStorageCfg")
	flags.BoolVar(&opts.config.WipeFirst, "uninstall-first", opts.config.WipeFirst, "Uninstall the namespace before install")
	flags.BoolVar(&opts.config.WipeFirst, "wipe-first", opts.config.WipeFirst, "Deprecated alias for --uninstall-first")
	flags.BoolVar(&opts.nonInteractive, "non-interactive", false, "Disable prompts and require flags/env vars")
	flags.BoolVarP(&opts.yes, "yes", "y", false, "Skip destructive confirmation checks for non-interactive uninstall flows")

	return command
}

func (o *installOptions) run(ctx context.Context, root *RootOptions) error {
	runner := executil.NewRunner()
	client := oc.NewClient(runner)

	paths, err := installer.DiscoverPaths(root.RepoRoot)
	if err != nil {
		return err
	}

	interactive := ui.IsInteractive() && !o.nonInteractive
	cfg := o.config
	cluster, err := ensureClusterLogin(ctx, runner, client, interactive)
	if err != nil {
		return err
	}

	discovery := discoverInstallDiscovery(ctx, client)

	var storageRanking preflight.StorageRanking
	if classes, err := client.StorageClasses(ctx); err == nil && len(classes) > 0 {
		storageRanking = preflight.RankStorageClasses(classes)
		if cfg.StorageClass == "" {
			cfg.StorageClass = storageRanking.Recommended
		}
		if cfg.ScimBridgeStorageClass == "" {
			cfg.ScimBridgeStorageClass = storageRanking.Recommended
		}
	}

	if interactive {
		ui.PrintClusterContext("Install", cluster.User, cluster.Server, cfg.Namespace, storageRanking.Recommended)
		ui.PrintInstallDiscovery(ui.InstallDiscoveryHints{
			MASRoutes:  discovery.MASRoutes,
			Workspaces: discovery.Workspaces,
		})
		cfg, err = ui.PromptInstall(cfg, ui.InstallDiscoveryHints{
			MASRoutes:  discovery.MASRoutes,
			Workspaces: discovery.Workspaces,
		}, storageRanking.Choices)
		if err != nil {
			return err
		}
	}
	if err := cfg.NormalizeComponents(); err != nil {
		return err
	}
	cfg.DefaultMASCoreNamespace()
	if err := cfg.Validate(); err != nil {
		return err
	}

	masBaseURLForPreflight := ""
	if cfg.HasComponent(config.InstallComponentSCIM) {
		masBaseURLForPreflight = cfg.MASBaseURL
	}
	report := preflight.Run(ctx, client, preflight.Input{
		Namespace:  cfg.Namespace,
		MASBaseURL: masBaseURLForPreflight,
	})
	preflight.Print(os.Stdout, report)
	if report.HasFailures() {
		return fmt.Errorf("preflight failed")
	}
	if cfg.StorageClass == "" && report.Storage.Recommended != "" {
		cfg.StorageClass = report.Storage.Recommended
	}
	if cfg.ScimBridgeStorageClass == "" && report.Storage.Recommended != "" {
		cfg.ScimBridgeStorageClass = report.Storage.Recommended
	}

	if interactive {
		ui.PrintInstallSummary(cfg)
		confirmed, err := ui.Confirm("Proceed with install?", true)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(os.Stdout, "[result] install cancelled")
			return nil
		}
	} else if cfg.WipeFirst && !o.yes {
		return fmt.Errorf("--yes is required with --uninstall-first in non-interactive mode")
	}

	output := io.Writer(os.Stdout)
	logPath := ""
	if logFile, path, err := logging.OpenRunLog(paths.RepoRoot, "install"); err == nil {
		defer logFile.Close()
		output = io.MultiWriter(os.Stdout, logFile)
		logPath = path
		fmt.Fprintf(os.Stdout, "[logs] writing execution log to %s\n", path)
	} else {
		fmt.Fprintf(os.Stderr, "[warn] unable to create log file: %v\n", err)
	}

	if cfg.WipeFirst {
		wipeConfig := config.WipeConfig{
			Namespace:        cfg.Namespace,
			ProfileID:        cfg.ProfileID,
			MASBaseURL:       cfg.MASBaseURL,
			MASAPITokenName:  cfg.MASAPITokenName,
			MASAPITokenValue: cfg.MASAPITokenValue,
		}
		fmt.Fprintf(output, "[install] uninstalling namespace %s before install\n", cfg.Namespace)
		if err := installer.RunWipe(ctx, runner, paths, wipeConfig, output); err != nil {
			if logPath != "" {
				fmt.Fprintf(os.Stderr, "[result] uninstall failed; see %s\n", logPath)
			}
			return err
		}
	}

	if cfg.HasComponent(config.InstallComponentLDAP) ||
		cfg.HasComponent(config.InstallComponentKeycloak) ||
		cfg.HasComponent(config.InstallComponentSCIM) {
		fmt.Fprintln(output, "[install] running scripts/install-all-in-one.sh")
		if err := installer.RunInstall(ctx, runner, paths, cfg, output); err != nil {
			if logPath != "" {
				fmt.Fprintf(os.Stderr, "[result] install failed; see %s\n", logPath)
			}
			return err
		}
	}

	if cfg.HasComponent(config.InstallComponentS3) {
		fmt.Fprintln(output, "[install] installing MinIO S3 service")
		opts := minioInstallOptions{
			masInstanceID:    cfg.MASInstanceID,
			masCoreNamespace: cfg.MASCoreNamespace,
			namespace:        cfg.Namespace,
			storageClassName: cfg.StorageClass,
			skipMASConfig:    cfg.SkipS3MASConfig,
			name:             defaultMinIOName,
			bucket:           defaultMinIOBucket,
			pvcSize:          defaultMinIOPVCSize,
			image:            defaultMinIOImage,
			mcImage:          defaultMinIOMCImage,
			timeout:          10 * time.Minute,
		}
		if err := opts.run(ctx); err != nil {
			if logPath != "" {
				fmt.Fprintf(os.Stderr, "[result] S3 install failed; see %s\n", logPath)
			}
			return err
		}
	}

	if cfg.HasComponent(config.InstallComponentSMTP) {
		fmt.Fprintln(output, "[install] installing Mailpit SMTP capture service")
		opts := mailpitInstallOptions{
			namespace: cfg.Namespace,
			name:      defaultMailpitName,
			image:     defaultMailpitImage,
			timeout:   5 * time.Minute,
		}
		if err := opts.run(ctx); err != nil {
			if logPath != "" {
				fmt.Fprintf(os.Stderr, "[result] SMTP install failed; see %s\n", logPath)
			}
			return err
		}
	}

	fmt.Fprintln(output, "[result] install completed")
	fmt.Fprintf(output, "[result] verify with: oc get pods -n %s\n", cfg.Namespace)
	fmt.Fprintf(output, "[result] logs shortcut: mas-est logs --namespace %s --component %s\n", cfg.Namespace, installLogComponent(cfg))

	return nil
}

func installLogComponent(cfg config.InstallConfig) string {
	switch {
	case cfg.HasComponent(config.InstallComponentSCIM):
		return "bridge"
	case cfg.HasComponent(config.InstallComponentSMTP):
		return "smtp"
	case cfg.HasComponent(config.InstallComponentS3):
		return "minio"
	case cfg.HasComponent(config.InstallComponentKeycloak):
		return "keycloak"
	case cfg.HasComponent(config.InstallComponentLDAP):
		return "openldap"
	default:
		return "bridge"
	}
}

func discoverInstallDiscovery(ctx context.Context, client *oc.Client) installDiscovery {
	discovery := installDiscovery{}

	if routes, err := client.MASRoutes(ctx); err == nil {
		discovery.MASRoutes = routes
	}
	if workspaces, err := client.ManageWorkspaces(ctx); err == nil {
		discovery.Workspaces = workspaces
	}

	return discovery
}
