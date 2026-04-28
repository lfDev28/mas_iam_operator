package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	DefaultNamespace           = "iam"
	DefaultProfileID           = "demo"
	DefaultKeycloakBootstrap   = "script"
	EnvNamespace               = "MAS_IAM_NAMESPACE"
	EnvMASBaseURL              = "SCIM_BRIDGE_MAS_BASE_URL"
	EnvMASAPITokenName         = "SCIM_BRIDGE_MAS_API_TOKEN_NAME"
	EnvMASAPITokenValue        = "SCIM_BRIDGE_MAS_API_TOKEN_VALUE"
	EnvWorkspaceID             = "SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID"
	EnvProfileID               = "SCIM_BRIDGE_MAS_PROFILE_ID"
	EnvStorageClass            = "POSTGRES_STORAGE_CLASS"
	EnvSCIMBridgeStorageClass  = "SCIM_BRIDGE_STORAGE_CLASS"
	EnvKeycloakBootstrapMethod = "SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_METHOD"
	EnvWipeFirst               = "MAS_IAM_WIPE_FIRST"
	EnvSkipProfileDelete       = "MAS_IAM_SKIP_PROFILE_DELETE"
	EnvRepoRoot                = "MAS_IAM_REPO_ROOT"
	EnvRendererBinary          = "MAS_IAM_RENDERER_BINARY"
)

type InstallConfig struct {
	Namespace               string
	MASBaseURL              string
	MASAPITokenName         string
	MASAPITokenValue        string
	WorkspaceID             string
	ProfileID               string
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
	cfg.Namespace = envOrDefault(EnvNamespace, cfg.Namespace)
	cfg.MASBaseURL = envOrDefault(EnvMASBaseURL, cfg.MASBaseURL)
	cfg.MASAPITokenName = envOrDefault(EnvMASAPITokenName, cfg.MASAPITokenName)
	cfg.MASAPITokenValue = envOrDefault(EnvMASAPITokenValue, cfg.MASAPITokenValue)
	cfg.WorkspaceID = envOrDefault(EnvWorkspaceID, cfg.WorkspaceID)
	cfg.ProfileID = envOrDefault(EnvProfileID, cfg.ProfileID)
	cfg.StorageClass = envOrDefault(EnvStorageClass, cfg.StorageClass)
	cfg.ScimBridgeStorageClass = envOrDefault(EnvSCIMBridgeStorageClass, cfg.ScimBridgeStorageClass)
	cfg.KeycloakBootstrapMethod = envOrDefault(EnvKeycloakBootstrapMethod, cfg.KeycloakBootstrapMethod)
	cfg.WipeFirst = boolEnvOrDefault(EnvWipeFirst, cfg.WipeFirst)
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
	cfg.Namespace = envOrDefault(EnvNamespace, cfg.Namespace)
	cfg.ProfileID = envOrDefault(EnvProfileID, cfg.ProfileID)
	cfg.SkipProfileDelete = boolEnvOrDefault(EnvSkipProfileDelete, cfg.SkipProfileDelete)
	cfg.MASBaseURL = envOrDefault(EnvMASBaseURL, cfg.MASBaseURL)
	cfg.MASAPITokenName = envOrDefault(EnvMASAPITokenName, cfg.MASAPITokenName)
	cfg.MASAPITokenValue = envOrDefault(EnvMASAPITokenValue, cfg.MASAPITokenValue)
	return cfg
}

func (c InstallConfig) Validate() error {
	missing := []string{}
	if strings.TrimSpace(c.Namespace) == "" {
		missing = append(missing, "--namespace / "+EnvNamespace)
	}
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
	return fmt.Errorf("missing required wipe values: %s", strings.Join(missing, ", "))
}

func (c InstallConfig) ScriptEnv() map[string]string {
	env := map[string]string{
		EnvMASBaseURL:       c.MASBaseURL,
		EnvMASAPITokenName:  c.MASAPITokenName,
		EnvMASAPITokenValue: c.MASAPITokenValue,
		EnvWorkspaceID:      c.WorkspaceID,
		EnvProfileID:        c.ProfileID,
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
