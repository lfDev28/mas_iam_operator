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
	inCluster      bool
	jobName        string
	installerImage string
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
	flags.BoolVar(&opts.config.ConfigureMASAuth, "configure-mas-auth", opts.config.ConfigureMASAuth, "Create MAS LDAP, OIDC, and SAML IDPCfg resources for the installed IAM services")
	flags.StringSliceVar(&opts.config.MASAuthProviders, "mas-auth-providers", opts.config.MASAuthProviders, "Comma-separated MAS auth providers to create: ldap,oidc,saml")
	flags.StringVar(&opts.config.MASAuthHost, "mas-auth-host", opts.config.MASAuthHost, "MAS auth route host for MAS auth auto-configuration")
	flags.StringVar(&opts.config.MASAuthInstanceID, "mas-auth-instance-id", opts.config.MASAuthInstanceID, "MAS instance ID for MAS auth auto-configuration")
	flags.StringVar(&opts.config.MASAuthCoreNamespace, "mas-auth-core-namespace", opts.config.MASAuthCoreNamespace, "MAS core namespace for MAS auth auto-configuration")
	flags.BoolVar(&opts.config.MASAuthUseCRApply, "mas-auth-use-cr-apply", opts.config.MASAuthUseCRApply, "Fall back to applying IDPCfg CRs directly instead of using the MAS Admin API")
	flags.StringVar(&opts.config.StorageClass, "storage-class", opts.config.StorageClass, "PostgreSQL storage class override")
	flags.StringVar(&opts.config.ScimBridgeStorageClass, "scim-bridge-storage-class", opts.config.ScimBridgeStorageClass, "SCIM bridge PVC storage class override")
	flags.StringVar(&opts.config.KeycloakBootstrapMethod, "keycloak-bootstrap", opts.config.KeycloakBootstrapMethod, "Keycloak bootstrap mode passed to install-all-in-one.sh")
	flags.BoolVar(&opts.config.SkipS3MASConfig, "skip-s3-mas-config", opts.config.SkipS3MASConfig, "Install S3 storage without creating MAS ObjectStorageCfg")
	flags.StringVar(&opts.config.SMTPRelayHost, "smtp-relay-host", opts.config.SMTPRelayHost, "Mailpit upstream SMTP relay host; leave empty for capture-only")
	flags.IntVar(&opts.config.SMTPRelayPort, "smtp-relay-port", opts.config.SMTPRelayPort, "Mailpit upstream SMTP relay port (default 587 for STARTTLS)")
	flags.StringVar(&opts.config.SMTPRelayUsername, "smtp-relay-username", opts.config.SMTPRelayUsername, "Mailpit upstream SMTP relay username")
	flags.StringVar(&opts.config.SMTPRelayPassword, "smtp-relay-password", opts.config.SMTPRelayPassword, "Mailpit upstream SMTP relay password (or app password)")
	flags.StringVar(&opts.config.SMTPRelayFrom, "smtp-relay-from", opts.config.SMTPRelayFrom, "Envelope sender (Return-Path) used when Mailpit relays messages")
	flags.StringVar(&opts.config.SMTPRelayAuth, "smtp-relay-auth", opts.config.SMTPRelayAuth, "Mailpit upstream SMTP relay auth mechanism: plain, login, cram-md5, or none")
	flags.BoolVar(&opts.config.SMTPRelayStartTLS, "smtp-relay-starttls", opts.config.SMTPRelayStartTLS, "Use STARTTLS when talking to the Mailpit upstream relay (recommended on port 587)")
	flags.StringVar(&opts.config.SMTPRelayAllowedRcpt, "smtp-relay-allowed-recipients", opts.config.SMTPRelayAllowedRcpt, "Optional regex restricting which recipient addresses Mailpit relays; empty = relay everything captured")
	flags.StringVar(&opts.config.IDPCfgMemoryLimit, "idpcfg-memory-limit", opts.config.IDPCfgMemoryLimit, "Memory limit applied to {instance}-entitymgr-idpcfg via Suite CR podTemplates (default 2Gi). The MAS default 512Mi OOMKills the finalizer playbook during MAS auth configuration; set to 'off' to skip the bump.")
	flags.BoolVar(&opts.config.WipeFirst, "uninstall-first", opts.config.WipeFirst, "Uninstall the namespace before install")
	flags.BoolVar(&opts.config.WipeFirst, "wipe-first", opts.config.WipeFirst, "Deprecated alias for --uninstall-first")
	flags.BoolVar(&opts.inCluster, "in-cluster", false, "Run the install as a Kubernetes Job inside the cluster instead of from this machine, so it survives sleep, VPN drops, and closed terminals")
	flags.StringVar(&opts.jobName, "job-name", installerJobDefaultName, "Name of the in-cluster installer Job created by --in-cluster")
	flags.StringVar(&opts.installerImage, "installer-image", defaultInstallerImage(), "Image the --in-cluster Job runs; defaults to this CLI's own version")
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
	cfg.DefaultMASAuthTarget()
	if err := cfg.Validate(); err != nil {
		return err
	}
	// The installer Job lives in the namespace the uninstall would delete, so
	// combining the two would have the Job delete itself mid-run.
	if o.inCluster && cfg.WipeFirst {
		return fmt.Errorf("--in-cluster cannot be combined with --uninstall-first; run 'mas-est uninstall --namespace %s' first, then rerun with --in-cluster", cfg.Namespace)
	}

	masBaseURLForPreflight := ""
	apiTokenName := ""
	apiTokenValue := ""
	if cfg.HasComponent(config.InstallComponentSCIM) {
		masBaseURLForPreflight = cfg.MASBaseURL
		apiTokenName = cfg.MASAPITokenName
		apiTokenValue = cfg.MASAPITokenValue
	}
	// HasMASAuthProvider treats an empty provider list as "all three", so a
	// bare --configure-mas-auth counts as including oidc. The OIDC endpoint
	// probe needs the MAS URL even when no SCIM-backed component is selected.
	checkOIDCEndpoint := cfg.ConfigureMASAuth && cfg.HasMASAuthProvider(config.MASAuthProviderOIDC)
	if checkOIDCEndpoint && masBaseURLForPreflight == "" {
		masBaseURLForPreflight = cfg.MASBaseURL
	}
	report := preflight.Run(ctx, client, preflight.Input{
		Namespace:         cfg.Namespace,
		MASBaseURL:        masBaseURLForPreflight,
		CheckOIDCEndpoint: checkOIDCEndpoint,
		APITokenName:      apiTokenName,
		APITokenValue:     apiTokenValue,
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

	if o.inCluster {
		inCluster := &inClusterInstallOptions{
			cfg:         cfg,
			jobName:     o.jobName,
			image:       o.installerImage,
			interactive: interactive,
			out:         os.Stdout,
		}
		return inCluster.run(ctx, client)
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

	phases := newPhaseTracker(output)
	installStart := time.Now()

	if cfg.WipeFirst {
		wipeConfig := config.WipeConfig{
			Namespace:        cfg.Namespace,
			ProfileID:        cfg.ProfileID,
			MASBaseURL:       cfg.MASBaseURL,
			MASAPITokenName:  cfg.MASAPITokenName,
			MASAPITokenValue: cfg.MASAPITokenValue,
		}
		if err := phases.run(fmt.Sprintf("Uninstall namespace %s", cfg.Namespace), func() error {
			return installer.RunWipe(ctx, runner, paths, wipeConfig, output)
		}); err != nil {
			if logPath != "" {
				fmt.Fprintf(os.Stderr, "[result] uninstall failed; see %s\n", logPath)
			}
			return err
		}
	}

	if cfg.HasComponent(config.InstallComponentLDAP) ||
		cfg.HasComponent(config.InstallComponentKeycloak) ||
		cfg.HasComponent(config.InstallComponentSCIM) {
		if err := phases.run("Install LDAP, Keycloak, SCIM bridge", func() error {
			return installer.RunInstall(ctx, runner, paths, cfg, output)
		}); err != nil {
			if logPath != "" {
				fmt.Fprintf(os.Stderr, "[result] install failed; see %s\n", logPath)
			}
			return err
		}
	}

	if cfg.HasComponent(config.InstallComponentS3) {
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
		if err := phases.run("Install MinIO S3", func() error { return opts.run(ctx) }); err != nil {
			if logPath != "" {
				fmt.Fprintf(os.Stderr, "[result] S3 install failed; see %s\n", logPath)
			}
			return err
		}
	}

	if cfg.HasComponent(config.InstallComponentSMTP) {
		opts := mailpitInstallOptions{
			namespace:        cfg.Namespace,
			name:             defaultMailpitName,
			image:            defaultMailpitImage,
			timeout:          5 * time.Minute,
			relayHost:        cfg.SMTPRelayHost,
			relayPort:        cfg.SMTPRelayPort,
			relayUsername:    cfg.SMTPRelayUsername,
			relayPassword:    cfg.SMTPRelayPassword,
			relayFrom:        cfg.SMTPRelayFrom,
			relayAuth:        cfg.SMTPRelayAuth,
			relayStartTLS:    cfg.SMTPRelayStartTLS,
			relayAllowedRcpt: cfg.SMTPRelayAllowedRcpt,
		}
		if err := phases.run("Install Mailpit SMTP", func() error { return opts.run(ctx) }); err != nil {
			if logPath != "" {
				fmt.Fprintf(os.Stderr, "[result] SMTP install failed; see %s\n", logPath)
			}
			return err
		}
	}

	if cfg.ConfigureMASAuth {
		opts := masAuthApplyOptions{
			namespace:        cfg.Namespace,
			release:          config.DefaultIAMRelease,
			realm:            defaultKeycloakRealm,
			masInstanceID:    cfg.MASAuthInstanceID,
			masCoreNamespace: cfg.MASAuthCoreNamespace,
			masAuthHost:      cfg.MASAuthHost,
			providers:        cfg.MASAuthProviders,
			ldapProviderID:   defaultMASAuthLDAPID,
			oidcProviderID:   defaultMASAuthOIDCID,
			samlProviderID:   defaultMASAuthSAMLID,
			wait:             true,
			timeout:          10 * time.Minute,

			useCRApply:         cfg.MASAuthUseCRApply,
			apiBaseURL:         cfg.MASBaseURL,
			apiTokenName:       cfg.MASAPITokenName,
			apiTokenValue:      cfg.MASAPITokenValue,
			insecureTLS:        true,
			selfRegWorkspaceID: cfg.WorkspaceID,
			idpcfgMemoryLimit:  cfg.IDPCfgMemoryLimit,
		}
		if err := phases.run("Configure MAS LDAP / OIDC / SAML", func() error { return opts.run(ctx) }); err != nil {
			if logPath != "" {
				fmt.Fprintf(os.Stderr, "[result] MAS auth configuration failed; see %s\n", logPath)
			}
			return err
		}
	}

	totalElapsed := time.Since(installStart)
	fmt.Fprintln(output, "[result] install completed")
	fmt.Fprintf(output, "[result] verify with: oc get pods -n %s\n", cfg.Namespace)
	fmt.Fprintf(output, "[result] logs shortcut: mas-est logs --namespace %s --component %s\n", cfg.Namespace, installLogComponent(cfg))
	fmt.Fprintf(output, "[result] connection details: mas-est details --namespace %s --component all\n", cfg.Namespace)
	printInstallSummary(output, phases.entries, totalElapsed, cfg)

	return nil
}

type phaseEntry struct {
	label   string
	elapsed time.Duration
	success bool
}

type phaseTracker struct {
	out     io.Writer
	entries []phaseEntry
}

func newPhaseTracker(out io.Writer) *phaseTracker {
	return &phaseTracker{out: out}
}

// run executes fn under a phase label, printing a ▶ header before and a ✓/✗
// footer with the elapsed time after. The step's own stdout output appears
// between the two markers (indented two spaces) so the user always knows
// which phase a particular log line belongs to.
func (p *phaseTracker) run(label string, fn func() error) error {
	fmt.Fprintf(p.out, "\n▶ %s\n", label)
	start := time.Now()
	err := fn()
	elapsed := time.Since(start)
	marker := "✓"
	if err != nil {
		marker = "✗"
	}
	fmt.Fprintf(p.out, "%s %s    (%s)\n", marker, label, ui.FormatElapsed(elapsed))
	p.entries = append(p.entries, phaseEntry{label: label, elapsed: elapsed, success: err == nil})
	return err
}

// printInstallSummary prints the post-install recap: a phase table with
// timings and a list of per-provider connection secrets the user can pull
// with a copy-paste oc get command.
func printInstallSummary(out io.Writer, entries []phaseEntry, total time.Duration, cfg config.InstallConfig) {
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Install summary (total %s)\n", ui.FormatElapsed(total))
	for _, entry := range entries {
		marker := "✓"
		if !entry.success {
			marker = "✗"
		}
		fmt.Fprintf(out, "  %s %-40s (%s)\n", marker, entry.label, ui.FormatElapsed(entry.elapsed))
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Connection details:")
	fmt.Fprintf(out, "  mas-est details --namespace %s --component all\n", cfg.Namespace)
	if cfg.ConfigureMASAuth {
		if cfg.HasMASAuthProvider(config.MASAuthProviderLDAP) {
			fmt.Fprintf(out, "  oc get secret %s -n %s -o yaml\n", ldapConnectionSecretName, cfg.Namespace)
		}
		if cfg.HasMASAuthProvider(config.MASAuthProviderOIDC) {
			fmt.Fprintf(out, "  oc get secret %s -n %s -o yaml\n", oidcConnectionSecretName, cfg.Namespace)
		}
		if cfg.HasMASAuthProvider(config.MASAuthProviderSAML) {
			fmt.Fprintf(out, "  oc get secret %s -n %s -o yaml\n", samlConnectionSecretName, cfg.Namespace)
		}
	}
	if cfg.HasComponent(config.InstallComponentS3) {
		fmt.Fprintf(out, "  oc get secret %s -n %s -o yaml\n", s3ConnectionSecretName, cfg.Namespace)
	}
	if cfg.HasComponent(config.InstallComponentSMTP) {
		fmt.Fprintf(out, "  oc get configmap %s -n %s -o yaml\n", smtpConnectionConfigMapName, cfg.Namespace)
	}
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
