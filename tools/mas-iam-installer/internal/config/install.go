package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	DefaultNamespace           = "mas-est"
	DefaultProfileID           = "demo"
	DefaultKeycloakBootstrap   = "script"
	DefaultIAMRelease          = "mas-est-iam"
	InstallComponentLDAP       = "ldap"
	InstallComponentKeycloak   = "keycloak"
	InstallComponentSCIM       = "scim"
	InstallComponentS3         = "s3"
	InstallComponentSMTP       = "smtp"
	EnvComponents              = "MAS_EST_COMPONENTS"
	EnvNamespace               = "MAS_EST_NAMESPACE"
	LegacyEnvNamespace         = "MAS_IAM_NAMESPACE"
	EnvMASBaseURL              = "SCIM_BRIDGE_MAS_BASE_URL"
	EnvMASAPITokenName         = "SCIM_BRIDGE_MAS_API_TOKEN_NAME"
	EnvMASAPITokenValue        = "SCIM_BRIDGE_MAS_API_TOKEN_VALUE"
	EnvWorkspaceID             = "SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID"
	EnvProfileID               = "SCIM_BRIDGE_MAS_PROFILE_ID"
	EnvStorageClass            = "POSTGRES_STORAGE_CLASS"
	EnvSCIMBridgeStorageClass  = "SCIM_BRIDGE_STORAGE_CLASS"
	EnvKeycloakBootstrapMethod = "SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_METHOD"
	EnvMASInstanceID           = "MAS_INSTANCE_ID"
	EnvMASCoreNamespace        = "MAS_CORE_NAMESPACE"
	EnvSkipS3MASConfig         = "MAS_EST_SKIP_S3_MAS_CONFIG"
	EnvUninstallFirst          = "MAS_EST_UNINSTALL_FIRST"
	EnvWipeFirst               = "MAS_EST_WIPE_FIRST"
	LegacyEnvWipeFirst         = "MAS_IAM_WIPE_FIRST"
	EnvSkipProfileDelete       = "MAS_EST_SKIP_PROFILE_DELETE"
	LegacyEnvSkipProfileDelete = "MAS_IAM_SKIP_PROFILE_DELETE"
	EnvRepoRoot                = "MAS_EST_REPO_ROOT"
	LegacyEnvRepoRoot          = "MAS_IAM_REPO_ROOT"
	EnvRendererBinary          = "MAS_EST_RENDERER_BINARY"
	LegacyEnvRendererBinary    = "MAS_IAM_RENDERER_BINARY"
)

type InstallConfig struct {
	Components              []string
	Namespace               string
	MASBaseURL              string
	MASAPITokenName         string
	MASAPITokenValue        string
	WorkspaceID             string
	ProfileID               string
	MASInstanceID           string
	MASCoreNamespace        string
	SkipS3MASConfig         bool
	StorageClass            string
	ScimBridgeStorageClass  string
	KeycloakBootstrapMethod string
	WipeFirst               bool
}

type WipeConfig struct {
	Namespace         string
	ProfileID         string
	SkipProfileDelete bool
	MASBaseURL        string
	MASAPITokenName   string
	MASAPITokenValue  string
}

func DefaultInstallConfig() InstallConfig {
	return InstallConfig{
		Namespace:               DefaultNamespace,
		ProfileID:               DefaultProfileID,
		KeycloakBootstrapMethod: DefaultKeycloakBootstrap,
	}
}

func LoadInstallConfigFromEnv() InstallConfig {
	cfg := DefaultInstallConfig()
	cfg.Components, _ = ParseInstallComponents(envOrDefault(EnvComponents, ""))
	cfg.Namespace = envOrDefaultWithLegacy(EnvNamespace, LegacyEnvNamespace, cfg.Namespace)
	cfg.MASBaseURL = envOrDefault(EnvMASBaseURL, cfg.MASBaseURL)
	cfg.MASAPITokenName = envOrDefault(EnvMASAPITokenName, cfg.MASAPITokenName)
	cfg.MASAPITokenValue = envOrDefault(EnvMASAPITokenValue, cfg.MASAPITokenValue)
	cfg.WorkspaceID = envOrDefault(EnvWorkspaceID, cfg.WorkspaceID)
	cfg.ProfileID = envOrDefault(EnvProfileID, cfg.ProfileID)
	cfg.MASInstanceID = envOrDefault(EnvMASInstanceID, cfg.MASInstanceID)
	cfg.MASCoreNamespace = envOrDefault(EnvMASCoreNamespace, cfg.MASCoreNamespace)
	cfg.SkipS3MASConfig = boolEnvOrDefault(EnvSkipS3MASConfig, cfg.SkipS3MASConfig)
	cfg.StorageClass = envOrDefault(EnvStorageClass, cfg.StorageClass)
	cfg.ScimBridgeStorageClass = envOrDefault(EnvSCIMBridgeStorageClass, cfg.ScimBridgeStorageClass)
	cfg.KeycloakBootstrapMethod = envOrDefault(EnvKeycloakBootstrapMethod, cfg.KeycloakBootstrapMethod)
	cfg.WipeFirst = boolEnvOrDefaultWithFallbacks(EnvUninstallFirst, cfg.WipeFirst, EnvWipeFirst, LegacyEnvWipeFirst)
	return cfg
}

func DefaultWipeConfig() WipeConfig {
	return WipeConfig{
		Namespace: DefaultNamespace,
		ProfileID: DefaultProfileID,
	}
}

func LoadWipeConfigFromEnv() WipeConfig {
	cfg := DefaultWipeConfig()
	cfg.Namespace = envOrDefaultWithLegacy(EnvNamespace, LegacyEnvNamespace, cfg.Namespace)
	cfg.ProfileID = envOrDefault(EnvProfileID, cfg.ProfileID)
	cfg.SkipProfileDelete = boolEnvOrDefaultWithLegacy(EnvSkipProfileDelete, LegacyEnvSkipProfileDelete, cfg.SkipProfileDelete)
	cfg.MASBaseURL = envOrDefault(EnvMASBaseURL, cfg.MASBaseURL)
	cfg.MASAPITokenName = envOrDefault(EnvMASAPITokenName, cfg.MASAPITokenName)
	cfg.MASAPITokenValue = envOrDefault(EnvMASAPITokenValue, cfg.MASAPITokenValue)
	return cfg
}

func (c InstallConfig) Validate() error {
	normalized, err := NormalizeInstallComponents(c.Components)
	if err != nil {
		return err
	}
	c.Components = normalized

	missing := []string{}
	if strings.TrimSpace(c.Namespace) == "" {
		missing = append(missing, "--namespace / "+EnvNamespace)
	}
	if !hasComponent(normalized, InstallComponentLDAP) &&
		!hasComponent(normalized, InstallComponentKeycloak) &&
		!hasComponent(normalized, InstallComponentSCIM) &&
		!hasComponent(normalized, InstallComponentS3) &&
		!hasComponent(normalized, InstallComponentSMTP) {
		missing = append(missing, "--components / "+EnvComponents)
	}
	if hasComponent(normalized, InstallComponentSCIM) {
		if strings.TrimSpace(c.MASBaseURL) == "" {
			missing = append(missing, "--mas-base-url / "+EnvMASBaseURL)
		}
		if strings.TrimSpace(c.MASAPITokenName) == "" {
			missing = append(missing, "--mas-api-token-name / "+EnvMASAPITokenName)
		}
		if strings.TrimSpace(c.MASAPITokenValue) == "" {
			missing = append(missing, "--mas-api-token-value / "+EnvMASAPITokenValue)
		}
		if strings.TrimSpace(c.WorkspaceID) == "" {
			missing = append(missing, "--workspace-id / "+EnvWorkspaceID)
		}
		if strings.TrimSpace(c.ProfileID) == "" {
			missing = append(missing, "--profile-id / "+EnvProfileID)
		}
	}
	if hasComponent(normalized, InstallComponentS3) && !c.SkipS3MASConfig {
		if strings.TrimSpace(c.MASInstanceID) == "" && strings.TrimSpace(c.MASCoreNamespace) == "" {
			missing = append(missing, "--mas-instance-id / "+EnvMASInstanceID)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("missing required install values: %s", strings.Join(missing, ", "))
}

func (c WipeConfig) Validate() error {
	missing := []string{}
	if strings.TrimSpace(c.Namespace) == "" {
		missing = append(missing, "--namespace / "+EnvNamespace)
	}
	if strings.TrimSpace(c.ProfileID) == "" {
		missing = append(missing, "--profile-id / "+EnvProfileID)
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("missing required uninstall values: %s", strings.Join(missing, ", "))
}

func (c InstallConfig) ScriptEnv() map[string]string {
	env := map[string]string{
		EnvComponents: InstallComponentsString(c.Components),
	}
	if c.MASBaseURL != "" {
		env[EnvMASBaseURL] = c.MASBaseURL
	}
	if c.MASAPITokenName != "" {
		env[EnvMASAPITokenName] = c.MASAPITokenName
	}
	if c.MASAPITokenValue != "" {
		env[EnvMASAPITokenValue] = c.MASAPITokenValue
	}
	if c.WorkspaceID != "" {
		env[EnvWorkspaceID] = c.WorkspaceID
	}
	if c.ProfileID != "" {
		env[EnvProfileID] = c.ProfileID
	}
	if c.StorageClass != "" {
		env[EnvStorageClass] = c.StorageClass
	}
	if c.ScimBridgeStorageClass != "" {
		env[EnvSCIMBridgeStorageClass] = c.ScimBridgeStorageClass
	}
	if c.KeycloakBootstrapMethod != "" {
		env[EnvKeycloakBootstrapMethod] = c.KeycloakBootstrapMethod
	}
	return env
}

func ParseInstallComponents(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	return NormalizeInstallComponents(strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}))
}

func NormalizeInstallComponents(components []string) ([]string, error) {
	selected := map[string]bool{}
	for _, component := range components {
		component = strings.ToLower(strings.TrimSpace(component))
		if component == "" {
			continue
		}
		switch component {
		case InstallComponentLDAP, InstallComponentKeycloak, InstallComponentSCIM, InstallComponentS3, InstallComponentSMTP:
			selected[component] = true
		default:
			return nil, fmt.Errorf("unknown install component %q; supported values are ldap, keycloak, scim, s3, smtp", component)
		}
	}

	if selected[InstallComponentSCIM] {
		selected[InstallComponentKeycloak] = true
		selected[InstallComponentLDAP] = true
	}
	if selected[InstallComponentKeycloak] {
		selected[InstallComponentLDAP] = true
	}

	order := []string{InstallComponentLDAP, InstallComponentKeycloak, InstallComponentSCIM, InstallComponentS3, InstallComponentSMTP}
	normalized := []string{}
	for _, component := range order {
		if selected[component] {
			normalized = append(normalized, component)
		}
	}
	return normalized, nil
}

func InstallComponentsString(components []string) string {
	normalized, err := NormalizeInstallComponents(components)
	if err != nil {
		return strings.Join(components, ",")
	}
	return strings.Join(normalized, ",")
}

func (c InstallConfig) HasComponent(component string) bool {
	normalized, err := NormalizeInstallComponents(c.Components)
	if err != nil {
		return false
	}
	return hasComponent(normalized, component)
}

func (c *InstallConfig) NormalizeComponents() error {
	components, err := NormalizeInstallComponents(c.Components)
	if err != nil {
		return err
	}
	c.Components = components
	return nil
}

func (c *InstallConfig) DefaultMASCoreNamespace() {
	c.MASInstanceID = strings.TrimSpace(c.MASInstanceID)
	c.MASCoreNamespace = strings.TrimSpace(c.MASCoreNamespace)
	if c.MASCoreNamespace == "" && c.MASInstanceID != "" {
		c.MASCoreNamespace = "mas-" + c.MASInstanceID + "-core"
	}
	if c.MASInstanceID == "" && strings.HasPrefix(c.MASCoreNamespace, "mas-") && strings.HasSuffix(c.MASCoreNamespace, "-core") {
		c.MASInstanceID = strings.TrimSuffix(strings.TrimPrefix(c.MASCoreNamespace, "mas-"), "-core")
	}
}

func hasComponent(components []string, component string) bool {
	for _, candidate := range components {
		if candidate == component {
			return true
		}
	}
	return false
}

func (c WipeConfig) ScriptEnv() map[string]string {
	env := map[string]string{}
	if c.MASBaseURL != "" {
		env[EnvMASBaseURL] = c.MASBaseURL
	}
	if c.MASAPITokenName != "" {
		env[EnvMASAPITokenName] = c.MASAPITokenName
	}
	if c.MASAPITokenValue != "" {
		env[EnvMASAPITokenValue] = c.MASAPITokenValue
	}
	if c.ProfileID != "" {
		env[EnvProfileID] = c.ProfileID
	}
	return env
}

func MaskSecret(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return "****"
	}
	return strings.Repeat("*", len(value)-4) + value[len(value)-4:]
}

func envOrDefault(name, fallback string) string {
	value, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func envOrDefaultWithLegacy(name, legacyName, fallback string) string {
	if value := envOrDefault(name, ""); value != "" {
		return value
	}
	return envOrDefault(legacyName, fallback)
}

func boolEnvOrDefault(name string, fallback bool) bool {
	value, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func boolEnvOrDefaultWithLegacy(name, legacyName string, fallback bool) bool {
	return boolEnvOrDefaultWithFallbacks(name, fallback, legacyName)
}

func boolEnvOrDefaultWithFallbacks(name string, fallback bool, fallbackNames ...string) bool {
	if value, ok := os.LookupEnv(name); ok && strings.TrimSpace(value) != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fallback
		}
		return parsed
	}
	for _, fallbackName := range fallbackNames {
		if value, ok := os.LookupEnv(fallbackName); ok && strings.TrimSpace(value) != "" {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fallback
			}
			return parsed
		}
	}
	return fallback
}
