package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/version"
)

func scimInstallConfig() config.InstallConfig {
	cfg := config.DefaultInstallConfig()
	cfg.Namespace = "mas-est"
	cfg.Components = []string{config.InstallComponentLDAP, config.InstallComponentKeycloak, config.InstallComponentSCIM}
	cfg.MASBaseURL = "https://api.mas.example.com/scim/v2"
	cfg.MASAPITokenName = "svc-scim-token"
	cfg.MASAPITokenValue = "s3cr3t-token"
	cfg.WorkspaceID = "ws1"
	cfg.MASInstanceID = "lfmas"
	cfg.MASCoreNamespace = "mas-lfmas-core"
	cfg.StorageClass = "ocs-storagecluster-ceph-rbd"
	cfg.ScimBridgeStorageClass = "ocs-storagecluster-ceph-rbd"
	return cfg
}

func newTestInClusterOptions(cfg config.InstallConfig) *inClusterInstallOptions {
	return &inClusterInstallOptions{
		cfg:     cfg,
		jobName: installerJobDefaultName,
		image:   defaultInstallerImage(),
	}
}

// jobSpec digs the Job out of the manifest set and returns its spec plus the
// single container, so shape assertions read as one lookup instead of five.
func jobSpec(t *testing.T, manifest map[string]any) (map[string]any, map[string]any) {
	t.Helper()
	spec, ok := manifest["spec"].(map[string]any)
	if !ok {
		t.Fatalf("job manifest has no spec: %v", manifest)
	}
	template := spec["template"].(map[string]any)
	podSpec := template["spec"].(map[string]any)
	containers := podSpec["containers"].([]map[string]any)
	if len(containers) != 1 {
		t.Fatalf("containers = %d, want 1", len(containers))
	}
	return spec, containers[0]
}

func TestInstallerJobManifestShape(t *testing.T) {
	opts := newTestInClusterOptions(scimInstallConfig())
	manifest := installerJobManifest(opts)

	metadata := manifest["metadata"].(map[string]any)
	if metadata["name"] != installerJobDefaultName {
		t.Fatalf("job name = %v, want %s", metadata["name"], installerJobDefaultName)
	}
	if metadata["namespace"] != "mas-est" {
		t.Fatalf("job namespace = %v, want mas-est", metadata["namespace"])
	}

	spec, container := jobSpec(t, manifest)
	if spec["backoffLimit"] != 0 {
		t.Fatalf("backoffLimit = %v, want 0", spec["backoffLimit"])
	}
	if spec["activeDeadlineSeconds"] != installerJobActiveDeadlineSeconds {
		t.Fatalf("activeDeadlineSeconds = %v, want %d", spec["activeDeadlineSeconds"], installerJobActiveDeadlineSeconds)
	}
	if _, ok := spec["ttlSecondsAfterFinished"]; ok {
		t.Fatal("ttlSecondsAfterFinished must stay unset so the finished Job remains the post-mortem artifact")
	}

	podSpec := spec["template"].(map[string]any)["spec"].(map[string]any)
	if podSpec["restartPolicy"] != "Never" {
		t.Fatalf("restartPolicy = %v, want Never", podSpec["restartPolicy"])
	}
	if podSpec["serviceAccountName"] != installerServiceAccountName {
		t.Fatalf("serviceAccountName = %v, want %s", podSpec["serviceAccountName"], installerServiceAccountName)
	}

	wantImage := "quay.io/lee_forster/mas-external-services-tool:v" + version.Version
	if container["image"] != wantImage {
		t.Fatalf("image = %v, want %s", container["image"], wantImage)
	}
}

func TestInstallerJobManifestUsesOverrideImage(t *testing.T) {
	opts := newTestInClusterOptions(scimInstallConfig())
	opts.image = "registry.example.com/mirror/mas-est:dev"

	_, container := jobSpec(t, installerJobManifest(opts))
	if container["image"] != "registry.example.com/mirror/mas-est:dev" {
		t.Fatalf("image = %v, want the --installer-image override", container["image"])
	}
}

func TestInstallerJobManifestKeepsSecretsOutOfTheSpec(t *testing.T) {
	cfg := scimInstallConfig()
	cfg.Components = append(cfg.Components, config.InstallComponentSMTP)
	cfg.SMTPRelayHost = "smtp.example.com"
	cfg.SMTPRelayPassword = "relay-app-password"
	opts := newTestInClusterOptions(cfg)

	// The whole Job spec is what shows up in `oc get job -o yaml`, so assert on
	// the serialized form rather than just the args slice.
	raw, err := json.Marshal(installerJobManifest(opts))
	if err != nil {
		t.Fatalf("marshal job manifest: %v", err)
	}
	for _, secret := range []string{cfg.MASAPITokenValue, cfg.MASAPITokenName, cfg.SMTPRelayPassword} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("job manifest leaks secret %q:\n%s", secret, raw)
		}
	}

	_, container := jobSpec(t, installerJobManifest(opts))
	env := container["env"].([]map[string]any)
	wired := map[string]string{}
	for _, entry := range env {
		valueFrom, ok := entry["valueFrom"].(map[string]any)
		if !ok {
			continue
		}
		ref := valueFrom["secretKeyRef"].(map[string]string)
		if ref["name"] != installerTokenSecretName {
			t.Fatalf("secretKeyRef name = %q, want %s", ref["name"], installerTokenSecretName)
		}
		wired[entry["name"].(string)] = ref["key"]
	}

	for _, key := range []string{config.EnvMASAPITokenName, config.EnvMASAPITokenValue, config.EnvSMTPRelayPassword} {
		if wired[key] != key {
			t.Fatalf("env %s not wired via secretKeyRef (got %q)", key, wired[key])
		}
	}
}

func TestInstallerSecretManifestCarriesOnlySetValues(t *testing.T) {
	cfg := config.DefaultInstallConfig()
	cfg.Namespace = "mas-est"
	cfg.Components = []string{config.InstallComponentLDAP}

	if data := installerSecretData(cfg); len(data) != 0 {
		t.Fatalf("installerSecretData() = %v, want empty for an LDAP-only install", data)
	}

	opts := newTestInClusterOptions(cfg)
	for _, manifest := range opts.manifests() {
		if manifest["kind"] == "Secret" {
			t.Fatal("no credentials Secret should be created when there is nothing secret to carry")
		}
	}

	data := installerSecretData(scimInstallConfig())
	if data[config.EnvMASAPITokenValue] != "s3cr3t-token" {
		t.Fatalf("secret data = %v, want the MAS API token value", data)
	}
	if _, ok := data[config.EnvSMTPRelayPassword]; ok {
		t.Fatalf("secret data = %v, want no SMTP relay password when none is set", data)
	}
}

func TestInstallerJobArgs(t *testing.T) {
	smtpRelay := func() config.InstallConfig {
		cfg := config.DefaultInstallConfig()
		cfg.Namespace = "mas-est"
		cfg.Components = []string{config.InstallComponentSMTP}
		cfg.SMTPRelayHost = "smtp.gmail.com"
		cfg.SMTPRelayUsername = "lab@example.com"
		cfg.SMTPRelayPassword = "app-password"
		cfg.SMTPRelayFrom = "lab@example.com"
		return cfg
	}
	masAuth := func() config.InstallConfig {
		cfg := scimInstallConfig()
		cfg.ConfigureMASAuth = true
		cfg.MASAuthProviders = []string{config.MASAuthProviderOIDC, config.MASAuthProviderLDAP}
		cfg.MASAuthHost = "auth.mas.example.com"
		cfg.MASAuthInstanceID = "lfmas"
		cfg.MASAuthCoreNamespace = "mas-lfmas-core"
		return cfg
	}

	tests := []struct {
		name    string
		cfg     config.InstallConfig
		want    []string
		absent  []string
		wantSub []string
	}{
		{
			name: "scim install",
			cfg:  scimInstallConfig(),
			want: []string{
				"install", "--non-interactive",
				"--namespace", "mas-est",
				"--components", "ldap,keycloak,scim",
				"--mas-base-url", "https://api.mas.example.com/scim/v2",
				"--workspace-id", "ws1",
				"--scim-bridge-storage-class", "ocs-storagecluster-ceph-rbd",
			},
			absent: []string{
				"--mas-api-token-name", "--mas-api-token-value", "svc-scim-token", "s3cr3t-token",
				"--configure-mas-auth", "--skip-s3-mas-config", "--smtp-relay-host",
			},
		},
		{
			name: "ldap only omits scim and mas auth flags",
			cfg: func() config.InstallConfig {
				c := config.DefaultInstallConfig()
				c.Namespace = "lab"
				c.Components = []string{config.InstallComponentLDAP}
				return c
			}(),
			want:   []string{"--components", "ldap", "--namespace", "lab"},
			absent: []string{"--mas-base-url", "--workspace-id", "--scim-bridge-storage-class", "--configure-mas-auth"},
		},
		{
			name:    "smtp relay omits the password",
			cfg:     smtpRelay(),
			want:    []string{"--smtp-relay-host", "smtp.gmail.com", "--smtp-relay-port", "587", "--smtp-relay-username", "lab@example.com"},
			absent:  []string{"--smtp-relay-password", "app-password"},
			wantSub: []string{"--smtp-relay-starttls=true"},
		},
		{
			name:   "mas auth providers are normalized",
			cfg:    masAuth(),
			want:   []string{"--configure-mas-auth", "--mas-auth-providers", "ldap,oidc", "--mas-auth-host", "auth.mas.example.com"},
			absent: []string{"--mas-auth-use-cr-apply"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := installerJobArgs(tt.cfg)
			if args[0] != "install" || args[1] != "--non-interactive" {
				t.Fatalf("args must start with install --non-interactive, got %v", args[:2])
			}
			joined := strings.Join(args, " ")
			for i := 0; i+1 < len(tt.want); i += 2 {
				if !strings.Contains(joined, tt.want[i]+" "+tt.want[i+1]) {
					t.Fatalf("args missing %q %q\n%v", tt.want[i], tt.want[i+1], args)
				}
			}
			for _, sub := range tt.wantSub {
				if !strings.Contains(joined, sub) {
					t.Fatalf("args missing %q\n%v", sub, args)
				}
			}
			for _, absent := range tt.absent {
				for _, arg := range args {
					if arg == absent {
						t.Fatalf("args must not contain %q\n%v", absent, args)
					}
				}
			}
		})
	}
}

func TestInstallerRBACManifests(t *testing.T) {
	opts := newTestInClusterOptions(scimInstallConfig())

	kinds := map[string]map[string]any{}
	for _, manifest := range opts.manifests() {
		kinds[manifest["kind"].(string)] = manifest
	}
	for _, kind := range []string{"Namespace", "ServiceAccount", "ClusterRole", "ClusterRoleBinding", "Role", "RoleBinding", "Secret", "Job"} {
		if _, ok := kinds[kind]; !ok {
			t.Fatalf("manifest set is missing a %s", kind)
		}
	}

	binding := kinds["ClusterRoleBinding"]
	if name := binding["metadata"].(map[string]any)["name"]; name != installerRBACName+"-mas-est" {
		t.Fatalf("ClusterRoleBinding name = %v, want namespace-suffixed", name)
	}
	subject := binding["subjects"].([]map[string]string)[0]
	if subject["name"] != installerServiceAccountName || subject["namespace"] != "mas-est" {
		t.Fatalf("ClusterRoleBinding subject = %v", subject)
	}
	roleRef := kinds["RoleBinding"]["roleRef"].(map[string]string)
	if roleRef["kind"] != "Role" || roleRef["name"] != installerRBACName {
		t.Fatalf("RoleBinding roleRef = %v, want the namespaced installer Role", roleRef)
	}
}

func TestInstallerClusterRoleGrantsExecAndCrossNamespaceMASVerbs(t *testing.T) {
	rules := installerClusterRoleManifest()["rules"].([]map[string]any)

	granted := func(group, resource, verb string) bool {
		for _, rule := range rules {
			if !containsString(rule["apiGroups"].([]string), group) {
				continue
			}
			if !containsString(rule["resources"].([]string), resource) {
				continue
			}
			if containsString(rule["verbs"].([]string), verb) {
				return true
			}
		}
		return false
	}

	// scripts/link-scim-users-oidc.sh runs `oc exec … mongosh` in the MAS Mongo
	// pod, so losing this rule silently breaks SCIM identity linking.
	required := []struct{ group, resource, verb string }{
		{"", "pods", "list"},
		{"", "pods/exec", "create"},
		{"", "namespaces", "create"},
		{"apiextensions.k8s.io", "customresourcedefinitions", "create"},
		{"storage.k8s.io", "storageclasses", "list"},
		{"security.openshift.io", "securitycontextconstraints", "use"},
		{"operators.coreos.com", "subscriptions", "create"},
		{"packages.operators.coreos.com", "packagemanifests", "get"},
		{"config.mas.ibm.com", "idpcfgs", "create"},
		{"config.mas.ibm.com", "idpcfgs", "update"},
		{"core.mas.ibm.com", "suites", "patch"},
		{"", "secrets", "create"},
		{"route.openshift.io", "routes", "get"},
	}
	for _, want := range required {
		if !granted(want.group, want.resource, want.verb) {
			t.Fatalf("ClusterRole is missing %s on %s/%s", want.verb, want.group, want.resource)
		}
	}

	// Destructive verbs on secrets stay in the namespaced Role.
	if granted("", "secrets", "delete") {
		t.Fatal("ClusterRole must not grant cluster-wide secret deletion")
	}
}

func TestInstallerRoleCoversTargetNamespaceWorkloads(t *testing.T) {
	rules := installerRoleManifest("mas-est")["rules"].([]map[string]any)

	found := map[string]bool{}
	for _, rule := range rules {
		for _, resource := range rule["resources"].([]string) {
			if containsString(rule["verbs"].([]string), "delete") {
				found[resource] = true
			}
		}
	}
	for _, resource := range []string{"secrets", "configmaps", "deployments", "statefulsets", "jobs", "services", "routes", "persistentvolumeclaims", "masiamstacks"} {
		if !found[resource] {
			t.Fatalf("namespaced Role is missing full CRUD on %s", resource)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
