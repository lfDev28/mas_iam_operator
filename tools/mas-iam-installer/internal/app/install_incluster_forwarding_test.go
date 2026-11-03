package app

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
)

// clearInstallEnv blanks every env var LoadInstallConfigFromEnv reads, so the
// install command's flag defaults are the built-in ones (envOrDefault treats
// blank as unset).
func clearInstallEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		config.EnvComponents, config.EnvNamespace, config.LegacyEnvNamespace, config.EnvMASBaseURL,
		config.EnvMASAPITokenName, config.EnvMASAPITokenValue, config.EnvWorkspaceID, config.EnvProfileID,
		config.EnvStorageClass, config.EnvSCIMBridgeStorageClass, config.EnvKeycloakBootstrapMethod,
		config.EnvMASInstanceID, config.EnvMASCoreNamespace, config.EnvConfigureMASAuth, config.EnvMASAuthProviders,
		config.EnvMASAuthHost, config.EnvMASAuthCoreNamespace, config.EnvMASAuthInstanceID, config.EnvMASAuthUseCRApply,
		config.EnvSkipS3MASConfig, config.EnvSMTPRelayHost, config.EnvSMTPRelayPort, config.EnvSMTPRelayUsername,
		config.EnvSMTPRelayPassword, config.EnvSMTPRelayFrom, config.EnvSMTPRelayAuth, config.EnvSMTPRelayStartTLS,
		config.EnvSMTPRelayAllowedRcpt, config.EnvIDPCfgMemoryLimit, config.EnvUninstallFirst, config.EnvWipeFirst,
		config.LegacyEnvWipeFirst,
	} {
		t.Setenv(key, "")
	}
}

// everythingConfig sets every forwardable field to a distinctive non-default
// value, so a flag that fails to travel shows up as a default on the far side.
func everythingConfig() config.InstallConfig {
	cfg := config.DefaultInstallConfig()
	cfg.Namespace = "fwd-ns"
	cfg.Components = []string{config.InstallComponentLDAP, config.InstallComponentKeycloak, config.InstallComponentSCIM, config.InstallComponentS3, config.InstallComponentSMTP}
	cfg.MASBaseURL = "https://api.fwd.example.com/scim/v2"
	cfg.MASAPITokenName = "fwd-token-name"
	cfg.MASAPITokenValue = "fwd-token-value"
	cfg.WorkspaceID = "fwdws"
	cfg.ProfileID = "fwdprofile"
	cfg.MASInstanceID = "fwdinst"
	cfg.MASCoreNamespace = "mas-fwdinst-core"
	cfg.ConfigureMASAuth = true
	cfg.MASAuthHost = "auth.fwd.example.com"
	cfg.MASAuthCoreNamespace = "mas-fwdauth-core"
	cfg.MASAuthInstanceID = "fwdauth"
	cfg.MASAuthProviders = []string{config.MASAuthProviderLDAP, config.MASAuthProviderSAML}
	cfg.MASAuthUseCRApply = true
	cfg.SkipS3MASConfig = true
	cfg.StorageClass = "fwd-rbd"
	cfg.ScimBridgeStorageClass = "fwd-scim-rbd"
	cfg.KeycloakBootstrapMethod = "job"
	cfg.SMTPRelayHost = "smtp.fwd.example.com"
	cfg.SMTPRelayPort = 2525
	cfg.SMTPRelayUsername = "fwd-user"
	cfg.SMTPRelayPassword = "fwd-relay-password"
	cfg.SMTPRelayFrom = "fwd@example.com"
	cfg.SMTPRelayAuth = "login"
	cfg.SMTPRelayStartTLS = false
	cfg.SMTPRelayAllowedRcpt = ".*@fwd.example.com"
	cfg.IDPCfgMemoryLimit = "3Gi"
	return cfg
}

// forwardedInstallFlags is every `est install` flag the Job must receive, with
// the config value it must carry. A flag dropped launcher→Job has shipped in a
// release before; this table makes the next one fail a test.
var forwardedInstallFlags = []struct {
	flag string
	want func(config.InstallConfig) string
}{
	{"namespace", func(c config.InstallConfig) string { return c.Namespace }},
	{"components", func(c config.InstallConfig) string { return config.InstallComponentsString(c.Components) }},
	{"mas-base-url", func(c config.InstallConfig) string { return c.MASBaseURL }},
	{"workspace-id", func(c config.InstallConfig) string { return c.WorkspaceID }},
	{"profile-id", func(c config.InstallConfig) string { return c.ProfileID }},
	{"mas-instance-id", func(c config.InstallConfig) string { return c.MASInstanceID }},
	{"mas-core-namespace", func(c config.InstallConfig) string { return c.MASCoreNamespace }},
	{"configure-mas-auth", func(c config.InstallConfig) string { return strconv.FormatBool(c.ConfigureMASAuth) }},
	{"mas-auth-providers", func(c config.InstallConfig) string { return config.MASAuthProvidersString(c.MASAuthProviders) }},
	{"mas-auth-host", func(c config.InstallConfig) string { return c.MASAuthHost }},
	{"mas-auth-instance-id", func(c config.InstallConfig) string { return c.MASAuthInstanceID }},
	{"mas-auth-core-namespace", func(c config.InstallConfig) string { return c.MASAuthCoreNamespace }},
	{"mas-auth-use-cr-apply", func(c config.InstallConfig) string { return strconv.FormatBool(c.MASAuthUseCRApply) }},
	{"storage-class", func(c config.InstallConfig) string { return c.StorageClass }},
	{"scim-bridge-storage-class", func(c config.InstallConfig) string { return c.ScimBridgeStorageClass }},
	{"keycloak-bootstrap", func(c config.InstallConfig) string { return c.KeycloakBootstrapMethod }},
	{"skip-s3-mas-config", func(c config.InstallConfig) string { return strconv.FormatBool(c.SkipS3MASConfig) }},
	{"smtp-relay-host", func(c config.InstallConfig) string { return c.SMTPRelayHost }},
	{"smtp-relay-port", func(c config.InstallConfig) string { return strconv.Itoa(c.SMTPRelayPort) }},
	{"smtp-relay-username", func(c config.InstallConfig) string { return c.SMTPRelayUsername }},
	{"smtp-relay-from", func(c config.InstallConfig) string { return c.SMTPRelayFrom }},
	{"smtp-relay-auth", func(c config.InstallConfig) string { return c.SMTPRelayAuth }},
	{"smtp-relay-starttls", func(c config.InstallConfig) string { return strconv.FormatBool(c.SMTPRelayStartTLS) }},
	{"smtp-relay-allowed-recipients", func(c config.InstallConfig) string { return c.SMTPRelayAllowedRcpt }},
	{"idpcfg-memory-limit", func(c config.InstallConfig) string { return c.IDPCfgMemoryLimit }},
}

// notForwardedInstallFlags are install flags that deliberately do NOT travel
// as Job args, each with the reason.
var notForwardedInstallFlags = map[string]string{
	"mas-api-token-name":  "secret: travels via the credentials Secret",
	"mas-api-token-value": "secret: travels via the credentials Secret",
	"smtp-relay-password": "secret: travels via the credentials Secret",
	"uninstall-first":     "refused in-cluster (the Job would delete itself)",
	"wipe-first":          "deprecated alias of --uninstall-first",
	"in-cluster":          "the Job always runs --local",
	"local":               "always passed explicitly",
	"job-name":            "launcher-only",
	"installer-image":     "launcher-only",
	"skip-version-check":  "launcher-only",
	"non-interactive":     "always passed explicitly",
	"yes":                 "only meaningful with --uninstall-first",
}

func parseJobArgs(t *testing.T, args []string) *pflag.FlagSet {
	t.Helper()
	command := newInstallCommand(&RootOptions{})
	if err := command.Flags().Parse(args[1:]); err != nil {
		t.Fatalf("the Job's args do not parse: %v\n%v", err, args)
	}
	return command.Flags()
}

func flagValue(t *testing.T, flags *pflag.FlagSet, name string) string {
	t.Helper()
	flag := flags.Lookup(name)
	if flag == nil {
		t.Fatalf("install command has no --%s flag", name)
	}
	if flag.Value.Type() == "stringSlice" {
		values, err := flags.GetStringSlice(name)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(values, ",")
	}
	return flag.Value.String()
}

// TestInstallerJobArgsRoundTripEveryFlag feeds the Job's argv back through the
// real install flag parser and checks every forwardable value survives.
func TestInstallerJobArgsRoundTripEveryFlag(t *testing.T) {
	clearInstallEnv(t)
	cfg := everythingConfig()
	flags := parseJobArgs(t, installerJobArgs(cfg))
	for _, tc := range forwardedInstallFlags {
		t.Run(tc.flag, func(t *testing.T) {
			if got, want := flagValue(t, flags, tc.flag), tc.want(cfg); got != want {
				t.Fatalf("--%s in the Job = %q, want %q (dropped or mangled launcher→Job)", tc.flag, got, want)
			}
		})
	}
}

// TestEveryInstallFlagIsClassified fails when a new install flag is added
// without deciding whether the in-cluster Job needs it.
func TestEveryInstallFlagIsClassified(t *testing.T) {
	clearInstallEnv(t)
	forwarded := map[string]bool{}
	for _, tc := range forwardedInstallFlags {
		forwarded[tc.flag] = true
	}
	newInstallCommand(&RootOptions{}).Flags().VisitAll(func(flag *pflag.Flag) {
		if forwarded[flag.Name] {
			return
		}
		if _, ok := notForwardedInstallFlags[flag.Name]; ok {
			return
		}
		t.Errorf("install flag --%s is neither forwarded to the in-cluster Job (forwardedInstallFlags) nor listed as launcher-only (notForwardedInstallFlags)", flag.Name)
	})
}

// Regression: --mas-base-url was only forwarded with the scim component, but
// the local path hands it to mas-auth too.
func TestInstallerJobArgsForwardsMASBaseURLForAuthOnly(t *testing.T) {
	clearInstallEnv(t)
	cfg := config.DefaultInstallConfig()
	cfg.Namespace = "mas-est"
	cfg.Components = []string{config.InstallComponentLDAP, config.InstallComponentKeycloak}
	cfg.ConfigureMASAuth = true
	cfg.MASBaseURL = "https://api.auth-only.example.com/scim/v2"
	cfg.MASAuthInstanceID = "inst"
	cfg.WorkspaceID = "ws"
	flags := parseJobArgs(t, installerJobArgs(cfg))
	if got := flagValue(t, flags, "mas-base-url"); got != cfg.MASBaseURL {
		t.Fatalf("--mas-base-url in the Job = %q, want %q", got, cfg.MASBaseURL)
	}
}

func TestInstallerJobForwardsPassthroughEnv(t *testing.T) {
	launcherEnv := map[string]string{}
	for _, key := range installerPassthroughEnv {
		launcherEnv[key] = "value-of-" + key
	}
	for _, key := range installerPassthroughSecretEnv {
		launcherEnv[key] = "secret-of-" + key
	}
	launcherEnv["UNRELATED_VAR"] = "must-not-travel"
	launcherEnv["SCIM_BRIDGE_INCLUDE_GROUPS"] = "  " // blank counts as unset
	lookup := func(key string) (string, bool) {
		value, ok := launcherEnv[key]
		return value, ok
	}

	opts := newTestInClusterOptions(scimInstallConfig())
	opts.env = collectInstallerPassthroughEnv(lookup)
	_, container := jobSpec(t, installerJobManifest(opts))
	plain := map[string]string{}
	fromSecret := map[string]string{}
	for _, entry := range container["env"].([]map[string]any) {
		name := entry["name"].(string)
		if value, ok := entry["value"].(string); ok {
			plain[name] = value
			continue
		}
		ref := entry["valueFrom"].(map[string]any)["secretKeyRef"].(map[string]string)
		fromSecret[name] = ref["key"]
	}

	for _, key := range installerPassthroughEnv {
		t.Run(key, func(t *testing.T) {
			if key == "SCIM_BRIDGE_INCLUDE_GROUPS" {
				if _, ok := plain[key]; ok {
					t.Fatal("a blank launcher value must not be forwarded")
				}
				return
			}
			if plain[key] != launcherEnv[key] {
				t.Fatalf("Job env %s = %q, want %q", key, plain[key], launcherEnv[key])
			}
		})
	}
	raw, _ := json.Marshal(installerJobManifest(opts))
	var secretData map[string]string
	for _, manifest := range opts.manifests() {
		if manifest["kind"] == "Secret" {
			secretData = manifest["stringData"].(map[string]string)
		}
	}
	for _, key := range installerPassthroughSecretEnv {
		t.Run(key, func(t *testing.T) {
			if fromSecret[key] != key {
				t.Fatalf("secret env %s not wired via secretKeyRef (got %q)", key, fromSecret[key])
			}
			if secretData[key] != launcherEnv[key] {
				t.Fatalf("credentials Secret %s = %q, want %q", key, secretData[key], launcherEnv[key])
			}
			if strings.Contains(string(raw), launcherEnv[key]) {
				t.Fatalf("Job spec leaks secret env %s", key)
			}
		})
	}
	if _, ok := plain["UNRELATED_VAR"]; ok {
		t.Fatal("unrelated launcher env must not be forwarded")
	}
}
