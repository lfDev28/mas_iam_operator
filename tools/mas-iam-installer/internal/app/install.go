package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	executil "github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/exec"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/installer"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/logging"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/masadmin"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/preflight"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/ui"
)

type installOptions struct {
	config         config.InstallConfig
	nonInteractive bool
	yes            bool
	inCluster      bool
	local          bool
	// skipVersionCheck bypasses the stale-runtime guard. Intended for
	// deliberately running a locally built binary against a published image.
	skipVersionCheck bool
	jobName          string
	installerImage   string
}

type installDiscovery struct {
	MASRoutes  []oc.MASRoute
	Workspaces []oc.ManageWorkspace
	// WorkspacesListed is false when listing ManageWorkspaces failed (no
	// RBAC, no Manage CRD), in which case a workspace id cannot be verified.
	WorkspacesListed bool
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
	flags.StringVar(&opts.config.WorkspaceID, "workspace-id", opts.config.WorkspaceID, "MAS workspace ID given to SCIM-synced and self-registered users")
	flags.StringVar(&opts.config.ProfileID, "profile-id", opts.config.ProfileID, "MAS profile ID")
	flags.StringVar(&opts.config.MASInstanceID, "mas-instance-id", opts.config.MASInstanceID, "MAS instance ID for S3 ObjectStorageCfg")
	flags.StringVar(&opts.config.MASCoreNamespace, "mas-core-namespace", opts.config.MASCoreNamespace, "MAS core namespace for S3 ObjectStorageCfg")
	flags.BoolVar(&opts.config.ConfigureMASAuth, "configure-mas-auth", opts.config.ConfigureMASAuth, "Create MAS LDAP, OIDC, and SAML IDPCfg resources for the installed IAM services")
	flags.StringSliceVar(&opts.config.MASAuthProviders, "mas-auth-providers", opts.config.MASAuthProviders, "Comma-separated MAS auth providers to create: ldap,oidc,saml")
	flags.StringVar(&opts.config.MASAuthHost, "mas-auth-host", opts.config.MASAuthHost, "MAS auth route host for MAS auth auto-configuration")
	flags.StringVar(&opts.config.MASAuthInstanceID, "mas-auth-instance-id", opts.config.MASAuthInstanceID, "MAS instance ID for MAS auth auto-configuration")
	flags.StringVar(&opts.config.MASAuthCoreNamespace, "mas-auth-core-namespace", opts.config.MASAuthCoreNamespace, "MAS core namespace for MAS auth auto-configuration")
	flags.BoolVar(&opts.config.MASAuthUseCRApply, "mas-auth-use-cr-apply", opts.config.MASAuthUseCRApply, "Fall back to applying IDPCfg CRs directly instead of using the MAS Admin API")
	flags.StringVar(&opts.config.StorageClass, "storage-class", opts.config.StorageClass, "Storage class for the PostgreSQL and OpenLDAP volumes")
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
	flags.BoolVar(&opts.inCluster, "in-cluster", true, "Default. Run the install as a Kubernetes Job inside the cluster, so it survives sleep, VPN drops, and closed terminals. Prompts and preflight still run on this machine. Pass --local to opt out")
	flags.BoolVar(&opts.local, "local", false, "Run the install from this machine instead of as an in-cluster Job. Overrides --in-cluster. Intended for development and debugging: the run dies with the terminal, the VPN, or a sleeping laptop")
	flags.StringVar(&opts.jobName, "job-name", installerJobDefaultName, "Name of the in-cluster installer Job")
	flags.StringVar(&opts.installerImage, "installer-image", defaultInstallerImage(), "Image the in-cluster installer Job runs; defaults to this CLI's own version")
	flags.BoolVar(&opts.skipVersionCheck, "skip-version-check", false, "Run even when MAS_EST_IMAGE names a different version than this binary")
	flags.BoolVar(&opts.nonInteractive, "non-interactive", false, "Disable prompts and require flags/env vars")
	flags.BoolVarP(&opts.yes, "yes", "y", false, "Skip destructive confirmation checks for non-interactive uninstall flows")

	return command
}

// runsInCluster decides which execution mode this invocation uses.
// --in-cluster defaults to true, so --local is the only way to get the
// on-this-machine path and always wins over --in-cluster.
func (o *installOptions) runsInCluster() bool {
	return o.inCluster && !o.local
}

func (o *installOptions) run(ctx context.Context, root *RootOptions) error {
	// Before anything touches the cluster: state the version, and refuse to run
	// a binary that disagrees with the image the user asked for. The in-cluster
	// installer's RBAC comes from this binary, so a stale launcher silently
	// reapplies stale permissions.
	printVersionBanner(os.Stdout)
	if err := checkRuntimeVersion(o.skipVersionCheck); err != nil {
		return err
	}

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
	fillMASAPICredentialsFromSecret(ctx, client, &cfg)

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
	// The prompt may have changed the namespace or switched MAS auth on.
	fillMASAPICredentialsFromSecret(ctx, client, &cfg)
	resolveMASAuthAPITarget(&cfg, discovery.MASRoutes)
	if err := cfg.Validate(); err != nil {
		return err
	}
	if cfg.HasComponent(config.InstallComponentSCIM) || cfg.ConfigureMASAuth {
		if err := checkWorkspaceExists(cfg.WorkspaceID, workspaceInstanceID(cfg, discovery.MASRoutes), discovery.Workspaces); err != nil {
			if !interactive {
				return err
			}
			fmt.Fprintf(os.Stderr, "[warn] %v\n", err)
		} else if !discovery.WorkspacesListed && strings.TrimSpace(cfg.WorkspaceID) != "" {
			fmt.Fprintf(os.Stderr, "[warn] could not list ManageWorkspaces; --workspace-id %q is not verified\n", cfg.WorkspaceID)
		}
	}
	// The installer Job lives in the namespace the uninstall would delete, so
	// combining the two would have the Job delete itself mid-run. In-cluster is
	// the default now, so the message must not blame a flag the user never
	// typed — it names the two ways out instead.
	if o.runsInCluster() && cfg.WipeFirst {
		return fmt.Errorf("--uninstall-first cannot run in the cluster: the installer Job lives in namespace %s, which the uninstall deletes, so the Job would remove itself mid-run.\n"+
			"  Either: run 'mas-est uninstall --namespace %s' first, then 'mas-est install' without --uninstall-first,\n"+
			"  or:     rerun with --local to do both from this machine.", cfg.Namespace, cfg.Namespace)
	}

	masBaseURLForPreflight := ""
	apiTokenName := ""
	apiTokenValue := ""
	// The MAS API key is used by the SCIM bridge AND by the default (Admin
	// API) --configure-mas-auth path; check it up front for both, so a bad
	// key or URL fails here instead of ~20 minutes into the install.
	if usesMASAdminAPI(cfg) {
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
	// The IDPCfg names are only knowable once --configure-mas-auth is on and
	// DefaultMASAuthTarget has resolved the instance id / core namespace above.
	// The provider ids are the fixed defaults the install path passes to
	// masAuthApplyOptions below; install exposes no flag to override them.
	report := preflight.Run(ctx, client, preflight.Input{
		Namespace:         cfg.Namespace,
		MASBaseURL:        masBaseURLForPreflight,
		CheckOIDCEndpoint: checkOIDCEndpoint,
		APITokenName:      apiTokenName,
		APITokenValue:     apiTokenValue,
		// Without scim the key only drives the MAS auth Admin API, which
		// needs no SCIM permissions.
		SkipSCIMAuthorization: !cfg.HasComponent(config.InstallComponentSCIM),
		CheckIDPCfgOverwrite:  cfg.ConfigureMASAuth,
		IDPCfgPlan: preflight.IDPCfgPlan{
			CoreNamespace:  cfg.MASAuthCoreNamespace,
			InstanceID:     cfg.MASAuthInstanceID,
			Providers:      cfg.MASAuthProviders,
			LDAPProviderID: defaultMASAuthLDAPID,
			OIDCProviderID: defaultMASAuthOIDCID,
			SAMLProviderID: defaultMASAuthSAMLID,
		},
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

	if o.runsInCluster() {
		inCluster := &inClusterInstallOptions{
			cfg:         cfg,
			jobName:     o.jobName,
			image:       o.installerImage,
			interactive: interactive,
			out:         os.Stdout,
			env:         collectInstallerPassthroughEnv(os.LookupEnv),
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
		discovery.WorkspacesListed = true
	}

	return discovery
}

// usesMASAdminAPI reports whether this install talks to the MAS API with the
// API key: the SCIM bridge does, and so does --configure-mas-auth unless it
// falls back to applying IDPCfg CRs.
func usesMASAdminAPI(cfg config.InstallConfig) bool {
	return cfg.HasComponent(config.InstallComponentSCIM) || (cfg.ConfigureMASAuth && !cfg.MASAuthUseCRApply)
}

// fillMASAPICredentialsFromSecret fills an unset MAS API key (and base URL)
// from the scim-bridge-secret an earlier SCIM install left in the target
// namespace — the same fallback `mas-est mas-auth apply` uses — so a re-run
// with --configure-mas-auth does not have to repeat them. Values given by
// flag or env always win. Silent when the secret does not exist.
func fillMASAPICredentialsFromSecret(ctx context.Context, client *oc.Client, cfg *config.InstallConfig) {
	if strings.TrimSpace(cfg.MASAPITokenName) != "" && strings.TrimSpace(cfg.MASAPITokenValue) != "" && strings.TrimSpace(cfg.MASBaseURL) != "" {
		return
	}
	if strings.TrimSpace(cfg.Namespace) == "" {
		return
	}
	data, err := client.SecretData(ctx, cfg.Namespace, scimBridgeSecretName)
	if err != nil {
		return
	}
	if applyStoredMASAPICredentials(cfg, data) {
		fmt.Fprintf(os.Stdout, "[config] using MAS API settings stored in secret/%s in namespace %s for values not given by flag or env\n", scimBridgeSecretName, cfg.Namespace)
	}
}

// applyStoredMASAPICredentials copies MAS API values from secret data into
// the fields cfg leaves empty; reports whether anything was copied.
func applyStoredMASAPICredentials(cfg *config.InstallConfig, data map[string]string) bool {
	changed := false
	fill := func(target *string, key string) {
		if strings.TrimSpace(*target) == "" && strings.TrimSpace(data[key]) != "" {
			*target = strings.TrimSpace(data[key])
			changed = true
		}
	}
	fill(&cfg.MASAPITokenName, config.EnvMASAPITokenName)
	fill(&cfg.MASAPITokenValue, config.EnvMASAPITokenValue)
	fill(&cfg.MASBaseURL, config.EnvMASBaseURL)
	return changed
}

// resolveMASAuthAPITarget gives the Admin API --configure-mas-auth path a MAS
// API URL when no SCIM component supplied one: the API route of the MAS auth
// instance, as `mas-est mas-auth apply` would discover it. The URL is
// normalised to carry /scim/v2 (stripped again by the Admin API client) so
// preflight and the in-cluster Job treat it like a SCIM base URL.
func resolveMASAuthAPITarget(cfg *config.InstallConfig, routes []oc.MASRoute) {
	if !cfg.ConfigureMASAuth || cfg.MASAuthUseCRApply || cfg.HasComponent(config.InstallComponentSCIM) {
		return
	}
	if strings.TrimSpace(cfg.MASBaseURL) == "" {
		for _, route := range routes {
			if route.InstanceID == cfg.MASAuthInstanceID {
				cfg.MASBaseURL = route.SCIMBaseURL()
				fmt.Fprintf(os.Stdout, "[config] MAS API URL for MAS auth configuration: %s (from route %s/%s; override with --mas-base-url)\n", cfg.MASBaseURL, route.Namespace, route.Name)
				break
			}
		}
	}
	if raw := strings.TrimSpace(cfg.MASBaseURL); raw != "" && !strings.Contains(raw, "/scim/v2") {
		cfg.MASBaseURL = masadmin.StripSCIMSuffix(raw) + "/scim/v2"
	}
}

// workspaceInstanceID is the MAS instance whose workspaces --workspace-id must
// belong to: the MAS auth instance when --configure-mas-auth is on, else the
// instance whose API route matches --mas-base-url.
func workspaceInstanceID(cfg config.InstallConfig, routes []oc.MASRoute) string {
	if cfg.ConfigureMASAuth && strings.TrimSpace(cfg.MASAuthInstanceID) != "" {
		return strings.TrimSpace(cfg.MASAuthInstanceID)
	}
	if host, err := preflight.ParseMASHost(cfg.MASBaseURL); err == nil {
		for _, route := range routes {
			if route.Host == host {
				return route.InstanceID
			}
		}
	}
	return strings.TrimSpace(cfg.MASInstanceID)
}

// checkWorkspaceExists verifies --workspace-id names a ManageWorkspace of the
// target instance. A typo otherwise surfaces much later: SCIM users land in a
// workspace that does not exist, and the selfreg ConfigMap sends first-time
// IDP users there. Returns nil when there is nothing to check against (no
// workspace id, or no ManageWorkspaces visible at all).
func checkWorkspaceExists(workspaceID, instanceID string, workspaces []oc.ManageWorkspace) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" || len(workspaces) == 0 {
		return nil
	}
	candidates := workspaces
	if instanceID != "" {
		scoped := []oc.ManageWorkspace{}
		for _, workspace := range workspaces {
			if workspace.InstanceID == instanceID {
				scoped = append(scoped, workspace)
			}
		}
		if len(scoped) > 0 {
			candidates = scoped
		}
	}
	available := make([]string, 0, len(candidates))
	for _, workspace := range candidates {
		if workspace.WorkspaceID == workspaceID {
			return nil
		}
		available = append(available, fmt.Sprintf("%s (instance %s, ManageWorkspace %s/%s)", workspace.WorkspaceID, workspace.InstanceID, workspace.Namespace, workspace.Name))
	}
	scope := "on this cluster"
	if instanceID != "" {
		scope = "for MAS instance " + instanceID
	}
	return fmt.Errorf("--workspace-id %q does not match any ManageWorkspace %s; available: %s", workspaceID, scope, strings.Join(available, ", "))
}
