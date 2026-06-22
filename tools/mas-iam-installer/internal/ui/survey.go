package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"golang.org/x/term"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/preflight"
)

func IsInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

type InstallDiscoveryHints struct {
	MASRoutes  []oc.MASRoute
	Workspaces []oc.ManageWorkspace
}

func PromptInstall(cfg config.InstallConfig, hints InstallDiscoveryHints, storageChoices []preflight.StorageChoice) (config.InstallConfig, error) {
	var err error

	if cfg.Namespace, err = askRequiredInput(
		"Namespace",
		cfg.Namespace,
		"OpenShift namespace for the MAS EST services. Press Enter to accept the current default.",
	); err != nil {
		return cfg, err
	}
	if cfg.Components, err = askInstallComponents(cfg.Components); err != nil {
		return cfg, err
	}

	selectedInstanceID := ""
	if cfg.HasComponent(config.InstallComponentSCIM) {
		if cfg.MASBaseURL, err = askMASBaseURL(cfg.MASBaseURL, hints.MASRoutes); err != nil {
			return cfg, err
		}
		if cfg.MASAPITokenName, err = askRequiredInput(
			"MAS API token name",
			cfg.MASAPITokenName,
			"SCIM API token name used to obtain the MAS JWT.",
		); err != nil {
			return cfg, err
		}

		if cfg.MASAPITokenValue == "" {
			if cfg.MASAPITokenValue, err = askRequiredSecret(
				"MAS API token value",
				"Secret value paired with the MAS API token name.",
			); err != nil {
				return cfg, err
			}
		} else {
			useExisting := true
			if err := askOne(&survey.Confirm{
				Message: "Reuse MAS API token value from env or flags?",
				Default: true,
				Help:    "Answer No only if you want to type a different secret now.",
			}, &useExisting); err != nil {
				return cfg, err
			}
			if !useExisting {
				if cfg.MASAPITokenValue, err = askRequiredSecret(
					"MAS API token value",
					"Secret value paired with the MAS API token name.",
				); err != nil {
					return cfg, err
				}
			}
		}

		selectedInstanceID = instanceIDForMASBaseURL(cfg.MASBaseURL, hints.MASRoutes)
		if cfg.WorkspaceID, err = askWorkspaceID(cfg.WorkspaceID, selectedInstanceID, hints.Workspaces); err != nil {
			return cfg, err
		}
		if cfg.ProfileID, err = askRequiredInput(
			"MAS profile ID",
			cfg.ProfileID,
			"Profile created or synced by the SCIM bridge. Defaults are fine for most first installs.",
		); err != nil {
			return cfg, err
		}
	}

	if cfg.HasComponent(config.InstallComponentS3) && !cfg.SkipS3MASConfig {
		if cfg.MASInstanceID == "" {
			cfg.MASInstanceID = selectedInstanceID
			if cfg.MASInstanceID == "" && len(hints.MASRoutes) == 1 {
				cfg.MASInstanceID = hints.MASRoutes[0].InstanceID
			}
		}
		if cfg.MASInstanceID, err = askRequiredInput(
			"MAS instance ID",
			cfg.MASInstanceID,
			"MAS instance that should receive the ObjectStorageCfg, for example lfmas.",
		); err != nil {
			return cfg, err
		}
		previousCoreNamespace := cfg.MASCoreNamespace
		cfg.DefaultMASCoreNamespace()
		if previousCoreNamespace == "" && cfg.MASCoreNamespace != "" {
			PrintDerived("MAS core namespace", cfg.MASCoreNamespace, "--mas-core-namespace")
		} else if cfg.MASCoreNamespace == "" {
			if cfg.MASCoreNamespace, err = askRequiredInput(
				"MAS core namespace",
				cfg.MASCoreNamespace,
				"Namespace containing the MAS core ObjectStorageCfg resources.",
			); err != nil {
				return cfg, err
			}
		}
	}

	if cfg.HasComponent(config.InstallComponentLDAP) {
		if err := askOne(&survey.Confirm{
			Message: "Configure MAS authentication providers?",
			Default: cfg.ConfigureMASAuth,
			Help:    "Creates MAS IDPCfg resources backed by the installed OpenLDAP and Keycloak services.",
		}, &cfg.ConfigureMASAuth); err != nil {
			return cfg, err
		}
	}
	if cfg.ConfigureMASAuth {
		if cfg.MASAuthProviders, err = askMASAuthProviders(cfg.MASAuthProviders, cfg); err != nil {
			return cfg, err
		}
		previousAuthInstanceID := cfg.MASAuthInstanceID
		if cfg.MASAuthInstanceID == "" {
			cfg.MASAuthInstanceID = selectedInstanceID
			if cfg.MASAuthInstanceID == "" && cfg.MASInstanceID != "" {
				cfg.MASAuthInstanceID = cfg.MASInstanceID
			}
			if cfg.MASAuthInstanceID == "" && len(hints.MASRoutes) == 1 {
				cfg.MASAuthInstanceID = hints.MASRoutes[0].InstanceID
			}
		}
		if previousAuthInstanceID == "" && cfg.MASAuthInstanceID != "" {
			PrintDerived("MAS auth instance ID", cfg.MASAuthInstanceID, "--mas-auth-instance-id")
		} else if cfg.MASAuthInstanceID == "" {
			if cfg.MASAuthInstanceID, err = askRequiredInput(
				"MAS auth instance ID",
				cfg.MASAuthInstanceID,
				"MAS instance that should receive LDAP, OIDC, and SAML IDPCfg resources.",
			); err != nil {
				return cfg, err
			}
		}
		previousAuthCoreNamespace := cfg.MASAuthCoreNamespace
		cfg.DefaultMASAuthTarget()
		if previousAuthCoreNamespace == "" && cfg.MASAuthCoreNamespace != "" {
			PrintDerived("MAS auth core namespace", cfg.MASAuthCoreNamespace, "--mas-auth-core-namespace")
		} else if cfg.MASAuthCoreNamespace == "" {
			if cfg.MASAuthCoreNamespace, err = askRequiredInput(
				"MAS auth core namespace",
				cfg.MASAuthCoreNamespace,
				"Namespace containing MAS CoreIDP and IDPCfg resources.",
			); err != nil {
				return cfg, err
			}
		}
		previousAuthHost := cfg.MASAuthHost
		if cfg.MASAuthHost == "" {
			cfg.MASAuthHost = authHostForInstance(cfg.MASAuthInstanceID, hints.MASRoutes)
		}
		if previousAuthHost == "" && cfg.MASAuthHost != "" {
			PrintDerived("MAS auth host", cfg.MASAuthHost, "--mas-auth-host")
		} else if cfg.MASAuthHost == "" {
			if cfg.MASAuthHost, err = askRequiredInput(
				"MAS auth host",
				cfg.MASAuthHost,
				"Host part of the MAS auth route, for example auth.<mas-domain>.",
			); err != nil {
				return cfg, err
			}
		}
	}

	if len(storageChoices) > 0 && (cfg.HasComponent(config.InstallComponentKeycloak) || cfg.HasComponent(config.InstallComponentS3)) {
		if cfg.StorageClass, err = askStorageClassSelect(
			"Primary storage class",
			"Choose the class for Keycloak PostgreSQL and/or MinIO PVCs. Block/RBD is usually the safer choice.",
			storageChoices,
			cfg.StorageClass,
		); err != nil {
			return cfg, err
		}
	} else if cfg.HasComponent(config.InstallComponentKeycloak) || cfg.HasComponent(config.InstallComponentS3) {
		if cfg.StorageClass, err = askOptionalInput(
			"Primary storage class",
			cfg.StorageClass,
			"Optional explicit override for Keycloak PostgreSQL and/or MinIO PVCs.",
		); err != nil {
			return cfg, err
		}
	}

	if cfg.HasComponent(config.InstallComponentSCIM) {
		if len(storageChoices) > 0 {
			if cfg.ScimBridgeStorageClass, err = askStorageClassSelect(
				"SCIM bridge storage class",
				"Choose the class for the scim-bridge-state PVC. You can use the same class as PostgreSQL.",
				storageChoices,
				cfg.ScimBridgeStorageClass,
			); err != nil {
				return cfg, err
			}
		} else {
			if cfg.ScimBridgeStorageClass, err = askOptionalInput(
				"SCIM bridge storage class",
				cfg.ScimBridgeStorageClass,
				"Optional explicit override for the scim-bridge-state PVC.",
			); err != nil {
				return cfg, err
			}
		}
	}

	if err := askOne(&survey.Confirm{
		Message: "Uninstall namespace before install?",
		Default: cfg.WipeFirst,
		Help:    "Use this when you want the CLI to remove the existing namespace before reinstalling.",
	}, &cfg.WipeFirst); err != nil {
		return cfg, err
	}

	return cfg, nil
}

func askInstallComponents(current []string) ([]string, error) {
	current, _ = config.NormalizeInstallComponents(current)
	labelForComponent := map[string]string{
		config.InstallComponentLDAP:     "LDAP directory",
		config.InstallComponentKeycloak: "Keycloak identity provider",
		config.InstallComponentSCIM:     "SCIM bridge",
		config.InstallComponentS3:       "S3 object storage",
		config.InstallComponentSMTP:     "SMTP Mailpit capture server",
	}
	componentForLabel := map[string]string{}
	options := []string{}
	for _, component := range []string{
		config.InstallComponentLDAP,
		config.InstallComponentKeycloak,
		config.InstallComponentSCIM,
		config.InstallComponentS3,
		config.InstallComponentSMTP,
	} {
		label := labelForComponent[component]
		componentForLabel[label] = component
		options = append(options, label)
	}
	defaultLabels := []string{}
	for _, component := range current {
		defaultLabels = append(defaultLabels, labelForComponent[component])
	}

	for {
		selectedLabels := defaultLabels
		if err := askOne(&survey.MultiSelect{
			Message: "Products to install",
			Options: options,
			Default: defaultLabels,
			Help:    "Use Space to select products. SCIM auto-selects Keycloak and LDAP; Keycloak auto-selects LDAP.",
		}, &selectedLabels); err != nil {
			return nil, err
		}
		selected := []string{}
		for _, label := range selectedLabels {
			selected = append(selected, componentForLabel[label])
		}
		normalized, err := config.NormalizeInstallComponents(selected)
		if err != nil {
			return nil, err
		}
		if len(normalized) > 0 {
			return normalized, nil
		}
		fmt.Println("Select at least one product.")
	}
}

func askMASAuthProviders(current []string, cfg config.InstallConfig) ([]string, error) {
	current, _ = config.NormalizeMASAuthProviders(current)

	labelForProvider := map[string]string{
		config.MASAuthProviderLDAP: "LDAP provider",
		config.MASAuthProviderOIDC: "OIDC provider (MAS 9.1+)",
		config.MASAuthProviderSAML: "SAML provider",
	}
	providerForLabel := map[string]string{}
	options := []string{}
	available := []string{}
	if cfg.HasComponent(config.InstallComponentLDAP) {
		available = append(available, config.MASAuthProviderLDAP)
	}
	if cfg.HasComponent(config.InstallComponentKeycloak) {
		available = append(available, config.MASAuthProviderOIDC, config.MASAuthProviderSAML)
	}
	for _, provider := range available {
		label := labelForProvider[provider]
		providerForLabel[label] = provider
		options = append(options, label)
	}

	defaultLabels := []string{}
	defaultProviders := current
	if len(defaultProviders) == 0 {
		defaultProviders = available
	}
	for _, provider := range defaultProviders {
		label := labelForProvider[provider]
		if label != "" {
			defaultLabels = append(defaultLabels, label)
		}
	}

	for {
		selectedLabels := defaultLabels
		if err := askOne(&survey.MultiSelect{
			Message: "MAS auth providers to create",
			Options: options,
			Default: defaultLabels,
			Help:    "Use Space to select providers. OIDC requires MAS 9.1 or later with IDPCfg spec.oidc support.",
		}, &selectedLabels); err != nil {
			return nil, err
		}
		selected := []string{}
		for _, label := range selectedLabels {
			selected = append(selected, providerForLabel[label])
		}
		normalized, err := config.NormalizeMASAuthProviders(selected)
		if err != nil {
			return nil, err
		}
		if len(normalized) > 0 {
			return normalized, nil
		}
		fmt.Println("Select at least one MAS auth provider.")
	}
}

func PrintInstallDiscovery(hints InstallDiscoveryHints) {
	lines := []string{}

	switch len(hints.MASRoutes) {
	case 1:
		route := hints.MASRoutes[0]
		lines = append(lines, fmt.Sprintf("detected MAS API route: %s (%s/%s)", route.SCIMBaseURL(), route.Namespace, route.Name))
	case 2, 3, 4, 5:
		lines = append(lines, fmt.Sprintf("detected %d MAS API routes; choose the correct one during install", len(hints.MASRoutes)))
	case 0:
	default:
		lines = append(lines, fmt.Sprintf("detected %d MAS API routes", len(hints.MASRoutes)))
	}

	switch len(hints.Workspaces) {
	case 1:
		workspace := hints.Workspaces[0]
		lines = append(lines, fmt.Sprintf("detected workspace hint: %s from ManageWorkspace %s", workspace.WorkspaceID, workspace.Name))
	case 2, 3, 4, 5:
		lines = append(lines, fmt.Sprintf("detected %d ManageWorkspace resources; workspace will be suggested after route selection", len(hints.Workspaces)))
	case 0:
	default:
		lines = append(lines, fmt.Sprintf("detected %d ManageWorkspace resources", len(hints.Workspaces)))
	}

	if len(lines) == 0 {
		return
	}

	PrintBanner("Detected", lines...)
}

func PromptWipe(cfg config.WipeConfig) (config.WipeConfig, error) {
	var err error

	if cfg.Namespace, err = askRequiredInput(
		"Namespace",
		cfg.Namespace,
		"OpenShift namespace to remove.",
	); err != nil {
		return cfg, err
	}
	if cfg.ProfileID, err = askRequiredInput(
		"MAS profile ID",
		cfg.ProfileID,
		"Profile identifier used for optional MAS-side profile deletion.",
	); err != nil {
		return cfg, err
	}
	if err := askOne(&survey.Confirm{
		Message: "Skip MAS profile delete?",
		Default: cfg.SkipProfileDelete,
		Help:    "Choose Yes if you only want to uninstall cluster resources and leave the MAS profile alone.",
	}, &cfg.SkipProfileDelete); err != nil {
		return cfg, err
	}

	return cfg, nil
}

func PromptPreflight(namespace, masBaseURL string) (string, string, error) {
	var err error

	if namespace, err = askRequiredInput(
		"Namespace",
		namespace,
		"Target namespace for the upcoming install or status check.",
	); err != nil {
		return "", "", err
	}
	if masBaseURL, err = askRequiredInput(
		"MAS SCIM base URL",
		masBaseURL,
		"Must include /scim/v2 so route lookup and CA detection target the correct endpoint.",
	); err != nil {
		return "", "", err
	}

	return namespace, masBaseURL, nil
}

func Confirm(message string, defaultValue bool) (bool, error) {
	confirmed := defaultValue
	err := askOne(&survey.Confirm{
		Message: message,
		Default: defaultValue,
	}, &confirmed)
	return confirmed, err
}

func askRequiredInput(message, defaultValue, help string) (string, error) {
	value := ""
	prompt := &survey.Input{
		Message: message,
		Default: defaultValue,
		Help:    help,
	}
	if err := askOne(prompt, &value, survey.WithValidator(survey.Required)); err != nil {
		return "", err
	}
	return value, nil
}

func askOptionalInput(message, defaultValue, help string) (string, error) {
	value := ""
	prompt := &survey.Input{
		Message: message,
		Default: defaultValue,
		Help:    help,
	}
	if err := askOne(prompt, &value); err != nil {
		return "", err
	}
	return value, nil
}

func askRequiredSecret(message, help string) (string, error) {
	value := ""
	if err := askOne(&survey.Password{
		Message: message,
		Help:    help,
	}, &value, survey.WithValidator(survey.Required)); err != nil {
		return "", err
	}
	return value, nil
}

func askMASBaseURL(current string, routes []oc.MASRoute) (string, error) {
	var err error

	if current == "" && len(routes) > 1 {
		if current, err = askMASRouteSelect(routes); err != nil {
			return "", err
		}
	}
	if current == "" && len(routes) == 1 {
		current = routes[0].SCIMBaseURL()
	}

	help := "Must include /scim/v2, for example https://api.<mas-host>/scim/v2."
	if len(routes) == 1 {
		help = fmt.Sprintf("Detected from route %s/%s. Must include /scim/v2.", routes[0].Namespace, routes[0].Name)
	} else if len(routes) > 1 {
		help = "Detected multiple MAS API routes on the cluster. A route was suggested above; edit it if needed."
	}

	return askRequiredInput("MAS SCIM base URL", current, help)
}

func askMASRouteSelect(routes []oc.MASRoute) (string, error) {
	options := make([]string, 0, len(routes)+1)
	values := map[string]string{}
	for _, route := range routes {
		label := fmt.Sprintf("%s (%s/%s)", route.SCIMBaseURL(), route.Namespace, route.Name)
		options = append(options, label)
		values[label] = route.SCIMBaseURL()
	}
	options = append(options, "Enter manually")

	selected := options[0]
	if err := askOne(&survey.Select{
		Message: "Detected MAS API routes",
		Options: options,
		Default: selected,
		Help:    "Choose the MAS instance you want to target, or pick Enter manually to type the SCIM URL yourself.",
	}, &selected); err != nil {
		return "", err
	}

	return values[selected], nil
}

func askWorkspaceID(current, instanceID string, workspaces []oc.ManageWorkspace) (string, error) {
	var err error

	candidates := workspaceCandidatesForInstance(instanceID, workspaces)
	if len(candidates) == 0 && len(workspaces) == 1 {
		candidates = workspaces
	}

	if current == "" && len(candidates) == 1 {
		derived := candidates[0].WorkspaceID
		PrintDerived("Workspace ID", derived, "--workspace-id")
		return derived, nil
	}

	if current == "" && len(candidates) > 1 {
		if current, err = askWorkspaceSelect(candidates); err != nil {
			return "", err
		}
	}

	help := "Workspace used by the MAS profile bootstrap job."
	if len(candidates) > 1 && instanceID != "" {
		help = fmt.Sprintf("Detected multiple workspaces for MAS instance %s. A workspace was suggested above; edit it if needed.", instanceID)
	} else if len(candidates) > 1 {
		help = "Detected multiple ManageWorkspace resources. A workspace was suggested above; edit it if needed."
	}

	return askRequiredInput("Workspace ID", current, help)
}

func askWorkspaceSelect(workspaces []oc.ManageWorkspace) (string, error) {
	options := make([]string, 0, len(workspaces)+1)
	values := map[string]string{}
	for _, workspace := range workspaces {
		label := fmt.Sprintf("%s (%s)", workspace.WorkspaceID, workspace.Name)
		options = append(options, label)
		values[label] = workspace.WorkspaceID
	}
	options = append(options, "Enter manually")

	selected := options[0]
	if err := askOne(&survey.Select{
		Message: "Detected workspace IDs",
		Options: options,
		Default: selected,
		Help:    "Choose the workspace you want to use for profile bootstrap, or pick Enter manually to type one yourself.",
	}, &selected); err != nil {
		return "", err
	}

	return values[selected], nil
}

func instanceIDForMASBaseURL(rawURL string, routes []oc.MASRoute) string {
	host, err := preflight.ParseMASHost(rawURL)
	if err != nil {
		return ""
	}
	for _, route := range routes {
		if route.Host == host {
			return route.InstanceID
		}
	}
	return ""
}

func workspaceCandidatesForInstance(instanceID string, workspaces []oc.ManageWorkspace) []oc.ManageWorkspace {
	if instanceID == "" {
		return nil
	}

	candidates := make([]oc.ManageWorkspace, 0, len(workspaces))
	for _, workspace := range workspaces {
		if workspace.InstanceID == instanceID {
			candidates = append(candidates, workspace)
		}
	}
	return candidates
}

func storageChoiceLabel(choices []preflight.StorageChoice, selected string) string {
	for _, choice := range choices {
		if choice.Name == selected {
			return choice.Display()
		}
	}
	return ""
}

func askStorageClassSelect(message, help string, choices []preflight.StorageChoice, current string) (string, error) {
	options := make([]string, 0, len(choices))
	descriptions := map[string]string{}
	defaultOption := ""
	for _, choice := range choices {
		label := choice.Name
		options = append(options, label)
		descriptions[label] = storageChoiceDescription(choice)
		if choice.Recommended {
			defaultOption = label
		}
	}
	if current != "" {
		defaultOption = current
	}
	if defaultOption == "" && len(options) > 0 {
		defaultOption = options[0]
	}

	selected := defaultOption
	if err := askOne(&survey.Select{
		Message: message,
		Options: options,
		Default: defaultOption,
		Help:    help,
		Description: func(value string, index int) string {
			return descriptions[value]
		},
	}, &selected); err != nil {
		return "", err
	}

	return selected, nil
}

func PrintInstallSummary(cfg config.InstallConfig) {
	fmt.Printf("[config] components=%s\n", config.InstallComponentsString(cfg.Components))
	fmt.Printf("[config] namespace=%s\n", cfg.Namespace)
	if cfg.HasComponent(config.InstallComponentSCIM) {
		fmt.Printf("[config] mas_base_url=%s\n", cfg.MASBaseURL)
		fmt.Printf("[config] mas_api_token_name=%s\n", cfg.MASAPITokenName)
		fmt.Printf("[config] mas_api_token_value=%s\n", config.MaskSecret(cfg.MASAPITokenValue))
		fmt.Printf("[config] workspace_id=%s\n", cfg.WorkspaceID)
		fmt.Printf("[config] profile_id=%s\n", cfg.ProfileID)
	}
	if cfg.HasComponent(config.InstallComponentS3) {
		fmt.Printf("[config] mas_instance_id=%s\n", cfg.MASInstanceID)
		fmt.Printf("[config] mas_core_namespace=%s\n", cfg.MASCoreNamespace)
		fmt.Printf("[config] s3_mas_config_enabled=%t\n", !cfg.SkipS3MASConfig)
	}
	if cfg.ConfigureMASAuth {
		fmt.Printf("[config] configure_mas_auth=true\n")
		fmt.Printf("[config] mas_auth_providers=%s\n", config.MASAuthProvidersString(cfg.MASAuthProviders))
		fmt.Printf("[config] mas_auth_instance_id=%s\n", cfg.MASAuthInstanceID)
		fmt.Printf("[config] mas_auth_core_namespace=%s\n", cfg.MASAuthCoreNamespace)
		fmt.Printf("[config] mas_auth_host=%s\n", cfg.MASAuthHost)
	}
	if cfg.StorageClass != "" {
		fmt.Printf("[config] primary_storage_class=%s\n", cfg.StorageClass)
	}
	if cfg.HasComponent(config.InstallComponentSCIM) && cfg.ScimBridgeStorageClass != "" {
		fmt.Printf("[config] scim_bridge_storage_class=%s\n", cfg.ScimBridgeStorageClass)
	}
	fmt.Printf("[config] uninstall_first=%t\n", cfg.WipeFirst)
}

func authHostForInstance(instanceID string, routes []oc.MASRoute) string {
	if instanceID == "" {
		return ""
	}
	for _, route := range routes {
		if route.InstanceID == instanceID {
			return strings.Replace(route.Host, "api.", "auth.", 1)
		}
	}
	return ""
}

// PrintDerived prints a one-line confirmation that a value was auto-derived
// from another known value (typically the MAS instance ID or a discovered
// route) rather than prompted for. The override flag is shown so a user who
// wants a different value knows how to set it on the next run.
func PrintDerived(label, value, overrideFlag string) {
	fmt.Fprintf(os.Stdout, "[derived] %s: %s (override with %s)\n", label, value, overrideFlag)
}

func PrintBanner(title string, lines ...string) {
	trimmed := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			trimmed = append(trimmed, line)
		}
	}

	fmt.Printf("\n== %s ==\n", title)
	for _, line := range trimmed {
		fmt.Printf("   %s\n", line)
	}
	fmt.Println()
}

func PrintClusterContext(action, user, server, namespace string, recommendedStorage string) {
	lines := []string{
		fmt.Sprintf("cluster: %s", server),
		fmt.Sprintf("user: %s", user),
		fmt.Sprintf("namespace: %s", namespace),
	}
	if recommendedStorage != "" {
		lines = append(lines, fmt.Sprintf("recommended storage: %s", recommendedStorage))
	}
	lines = append(lines,
		"Press Enter to accept defaults.",
		"Use arrow keys to choose storage classes. Type ? during a prompt for field help.",
	)
	PrintBanner(fmt.Sprintf("MAS EST %s", action), lines...)
}

func storageChoiceDescription(choice preflight.StorageChoice) string {
	parts := []string{}
	if choice.Recommended {
		parts = append(parts, "recommended")
	}
	if choice.IsDefault {
		parts = append(parts, "cluster default")
	}
	if choice.Reason != "" {
		parts = append(parts, choice.Reason)
	}
	return strings.Join(parts, ", ")
}

func askOne(prompt survey.Prompt, response interface{}, opts ...survey.AskOpt) error {
	allOpts := append(promptStylingOptions(), opts...)
	return survey.AskOne(prompt, response, allOpts...)
}

func promptStylingOptions() []survey.AskOpt {
	return []survey.AskOpt{
		survey.WithPageSize(10),
		survey.WithIcons(func(icons *survey.IconSet) {
			icons.Question.Text = ">"
			icons.Question.Format = "cyan+b"
			icons.Help.Text = "i"
			icons.Help.Format = "cyan"
			icons.Error.Text = "!"
			icons.Error.Format = "red+b"
			icons.SelectFocus.Text = ">"
			icons.SelectFocus.Format = "green+b"
		}),
	}
}
