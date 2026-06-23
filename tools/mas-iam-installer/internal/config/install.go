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
	MASAuthProviderLDAP        = "ldap"
	MASAuthProviderOIDC        = "oidc"
	MASAuthProviderSAML        = "saml"
	EnvComponents              = "MAS_EST_COMPONENTS"
	EnvMASAuthProviders        = "MAS_EST_AUTH_PROVIDERS"
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
	EnvConfigureMASAuth        = "MAS_EST_CONFIGURE_MAS_AUTH"
	EnvMASAuthHost             = "MAS_AUTH_HOST"
	EnvMASAuthCoreNamespace    = "MAS_AUTH_CORE_NAMESPACE"
	EnvMASAuthInstanceID       = "MAS_AUTH_INSTANCE_ID"
	EnvMASAuthUseCRApply       = "MAS_EST_AUTH_USE_CR_APPLY"
	EnvSkipS3MASConfig         = "MAS_EST_SKIP_S3_MAS_CONFIG"
	EnvSMTPRelayHost           = "MAS_EST_SMTP_RELAY_HOST"
	EnvSMTPRelayPort           = "MAS_EST_SMTP_RELAY_PORT"
	EnvSMTPRelayUsername       = "MAS_EST_SMTP_RELAY_USERNAME"
	EnvSMTPRelayPassword       = "MAS_EST_SMTP_RELAY_PASSWORD"
	EnvSMTPRelayFrom           = "MAS_EST_SMTP_RELAY_FROM"
	EnvSMTPRelayAuth           = "MAS_EST_SMTP_RELAY_AUTH"
	EnvSMTPRelayStartTLS       = "MAS_EST_SMTP_RELAY_STARTTLS"
	EnvSMTPRelayAllowedRcpt    = "MAS_EST_SMTP_RELAY_ALLOWED_RECIPIENTS"
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
	ConfigureMASAuth        bool
	MASAuthHost             string
	MASAuthCoreNamespace    string
	MASAuthInstanceID       string
	MASAuthProviders        []string
	MASAuthUseCRApply       bool
	SkipS3MASConfig         bool
	StorageClass            string
	ScimBridgeStorageClass  string
	KeycloakBootstrapMethod string
	WipeFirst               bool

	// SMTP relay (post-Mailpit-install delivery). When SMTPRelayHost is
	// non-empty, the Mailpit deployment is configured to forward captured
	// emails to the named upstream SMTP server. Defaults to capture-only
	// when SMTPRelayHost is empty.
	SMTPRelayHost           string
	SMTPRelayPort           int
	SMTPRelayUsername       string
	SMTPRelayPassword       string
	SMTPRelayFrom           string
	SMTPRelayAuth           string
	SMTPRelayStartTLS       bool
	SMTPRelayAllowedRcpt    string
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
		SMTPRelayPort:           587,
		SMTPRelayAuth:           "plain",
		SMTPRelayStartTLS:       true,
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
	cfg.ConfigureMASAuth = boolEnvOrDefault(EnvConfigureMASAuth, cfg.ConfigureMASAuth)
	cfg.MASAuthProviders, _ = ParseMASAuthProviders(envOrDefault(EnvMASAuthProviders, ""))
	cfg.MASAuthHost = envOrDefault(EnvMASAuthHost, cfg.MASAuthHost)
	cfg.MASAuthCoreNamespace = envOrDefault(EnvMASAuthCoreNamespace, cfg.MASAuthCoreNamespace)
	cfg.MASAuthInstanceID = envOrDefault(EnvMASAuthInstanceID, cfg.MASAuthInstanceID)
	cfg.MASAuthUseCRApply = boolEnvOrDefault(EnvMASAuthUseCRApply, cfg.MASAuthUseCRApply)
	cfg.SkipS3MASConfig = boolEnvOrDefault(EnvSkipS3MASConfig, cfg.SkipS3MASConfig)
	cfg.StorageClass = envOrDefault(EnvStorageClass, cfg.StorageClass)
	cfg.ScimBridgeStorageClass = envOrDefault(EnvSCIMBridgeStorageClass, cfg.ScimBridgeStorageClass)
	cfg.KeycloakBootstrapMethod = envOrDefault(EnvKeycloakBootstrapMethod, cfg.KeycloakBootstrapMethod)
	cfg.WipeFirst = boolEnvOrDefaultWithFallbacks(EnvUninstallFirst, cfg.WipeFirst, EnvWipeFirst, LegacyEnvWipeFirst)
	cfg.SMTPRelayHost = envOrDefault(EnvSMTPRelayHost, cfg.SMTPRelayHost)
	if v := envOrDefault(EnvSMTPRelayPort, ""); v != "" {
		if port, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && port > 0 {
			cfg.SMTPRelayPort = port
		}
	}
	cfg.SMTPRelayUsername = envOrDefault(EnvSMTPRelayUsername, cfg.SMTPRelayUsername)
	cfg.SMTPRelayPassword = envOrDefault(EnvSMTPRelayPassword, cfg.SMTPRelayPassword)
	cfg.SMTPRelayFrom = envOrDefault(EnvSMTPRelayFrom, cfg.SMTPRelayFrom)
	cfg.SMTPRelayAuth = envOrDefault(EnvSMTPRelayAuth, cfg.SMTPRelayAuth)
	cfg.SMTPRelayStartTLS = boolEnvOrDefault(EnvSMTPRelayStartTLS, cfg.SMTPRelayStartTLS)
	cfg.SMTPRelayAllowedRcpt = envOrDefault(EnvSMTPRelayAllowedRcpt, cfg.SMTPRelayAllowedRcpt)
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
	if c.ConfigureMASAuth {
		authProviders, err := NormalizeMASAuthProviders(c.MASAuthProviders)
		if err != nil {
			return err
		}
		if len(authProviders) == 0 {
			authProviders = DefaultMASAuthProviders()
		}
		if hasValue(authProviders, MASAuthProviderLDAP) && !hasComponent(normalized, InstallComponentLDAP) {
			missing = append(missing, "--components ldap")
		}
		if (hasValue(authProviders, MASAuthProviderOIDC) || hasValue(authProviders, MASAuthProviderSAML)) && !hasComponent(normalized, InstallComponentKeycloak) {
			missing = append(missing, "--components keycloak")
		}
		if strings.TrimSpace(c.MASAuthInstanceID) == "" && strings.TrimSpace(c.MASAuthCoreNamespace) == "" && strings.TrimSpace(c.MASInstanceID) == "" && strings.TrimSpace(c.MASCoreNamespace) == "" {
			missing = append(missing, "--mas-auth-instance-id / "+EnvMASAuthInstanceID)
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

func DefaultMASAuthProviders() []string {
	return []string{MASAuthProviderLDAP, MASAuthProviderOIDC, MASAuthProviderSAML}
}

func ParseMASAuthProviders(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	return NormalizeMASAuthProviders(strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}))
}

func NormalizeMASAuthProviders(providers []string) ([]string, error) {
	selected := map[string]bool{}
	for _, provider := range providers {
		provider = strings.ToLower(strings.TrimSpace(provider))
		if provider == "" {
			continue
		}
		switch provider {
		case MASAuthProviderLDAP, MASAuthProviderOIDC, MASAuthProviderSAML:
			selected[provider] = true
		default:
			return nil, fmt.Errorf("unknown MAS auth provider %q; supported values are ldap, oidc, saml", provider)
		}
	}

	normalized := []string{}
	for _, provider := range DefaultMASAuthProviders() {
		if selected[provider] {
			normalized = append(normalized, provider)
		}
	}
	return normalized, nil
}

func MASAuthProvidersString(providers []string) string {
	normalized, err := NormalizeMASAuthProviders(providers)
	if err != nil {
		return strings.Join(providers, ",")
	}
	return strings.Join(normalized, ",")
}

func InstallComponentsString(components []string) string {
	normalized, err := NormalizeInstallComponents(components)
	if err != nil {
		return strings.Join(components, ",")
	}
	return strings.Join(normalized, ",")
}

func (c InstallConfig) HasMASAuthProvider(provider string) bool {
	providers, err := NormalizeMASAuthProviders(c.MASAuthProviders)
	if err != nil {
		return false
	}
	if len(providers) == 0 && c.ConfigureMASAuth {
		providers = DefaultMASAuthProviders()
	}
	return hasValue(providers, provider)
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

func (c *InstallConfig) DefaultMASAuthTarget() {
	c.MASAuthInstanceID = strings.TrimSpace(c.MASAuthInstanceID)
	c.MASAuthCoreNamespace = strings.TrimSpace(c.MASAuthCoreNamespace)
	c.MASAuthHost = strings.TrimSpace(c.MASAuthHost)
	if c.MASAuthInstanceID == "" {
		c.MASAuthInstanceID = strings.TrimSpace(c.MASInstanceID)
	}
	if c.MASAuthCoreNamespace == "" {
		c.MASAuthCoreNamespace = strings.TrimSpace(c.MASCoreNamespace)
	}
	if c.MASAuthCoreNamespace == "" && c.MASAuthInstanceID != "" {
		c.MASAuthCoreNamespace = "mas-" + c.MASAuthInstanceID + "-core"
	}
	if c.MASAuthInstanceID == "" && strings.HasPrefix(c.MASAuthCoreNamespace, "mas-") && strings.HasSuffix(c.MASAuthCoreNamespace, "-core") {
		c.MASAuthInstanceID = strings.TrimSuffix(strings.TrimPrefix(c.MASAuthCoreNamespace, "mas-"), "-core")
	}
}

func hasComponent(components []string, component string) bool {
	return hasValue(components, component)
}

func hasValue(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
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
