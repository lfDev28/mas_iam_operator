package app

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
			if !containsString(args, "--local") {
				t.Fatalf("args must contain --local (see TestInstallerJobArgsAlwaysPassesLocal), got %v", args)
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

// TestInstallerJobArgsAlwaysPassesLocal guards the single most important
// invariant of the in-cluster path. The Job's container runs this same CLI,
// and `install --in-cluster` DEFAULTS TO TRUE, so an inner `est install`
// without --local would launch another installer Job, which would launch
// another, forever: unbounded Job recursion that fills the cluster and never
// installs anything. --local forces the on-this-machine execution path inside
// the pod, terminating the chain at depth 1.
func TestInstallerJobArgsAlwaysPassesLocal(t *testing.T) {
	configs := map[string]config.InstallConfig{
		"scim":     scimInstallConfig(),
		"defaults": config.DefaultInstallConfig(),
		"mas auth": func() config.InstallConfig {
			cfg := scimInstallConfig()
			cfg.ConfigureMASAuth = true
			cfg.MASAuthInstanceID = "lfmas"
			cfg.MASAuthCoreNamespace = "mas-lfmas-core"
			return cfg
		}(),
	}

	for name, cfg := range configs {
		t.Run(name, func(t *testing.T) {
			if !containsString(installerJobArgs(cfg), "--local") {
				t.Fatalf("installerJobArgs() omitted --local; the in-cluster install would recurse without bound\n%v", installerJobArgs(cfg))
			}
		})
	}

	// The rendered Job spec is what the pod actually executes, so assert on it
	// too rather than only on the args builder.
	_, container := jobSpec(t, installerJobManifest(newTestInClusterOptions(scimInstallConfig())))
	args, ok := container["args"].([]string)
	if !ok {
		t.Fatalf("container args are not []string: %#v", container["args"])
	}
	if !containsString(args, "--local") {
		t.Fatalf("job container args omitted --local: %v", args)
	}
	if containsString(args, "--in-cluster") {
		t.Fatalf("job container args must not re-enable the in-cluster path: %v", args)
	}

	// End-to-end on the flag parser: feed the Job's own argv back through the
	// install command exactly as the pod's `est` binary would, and assert the
	// resulting options resolve to the LOCAL path. This is what actually stops
	// the recursion — a --local that the parser rejected or ignored would not.
	command := newInstallCommand(&RootOptions{})
	if err := command.Flags().Parse(args[1:]); err != nil {
		t.Fatalf("the Job's own args do not parse: %v\n%v", err, args)
	}
	local, err := command.Flags().GetBool("local")
	if err != nil {
		t.Fatalf("local flag: %v", err)
	}
	inCluster, err := command.Flags().GetBool("in-cluster")
	if err != nil {
		t.Fatalf("in-cluster flag: %v", err)
	}
	inner := installOptions{inCluster: inCluster, local: local}
	if inner.runsInCluster() {
		t.Fatalf("re-parsing the Job's args yields an in-cluster install; the Job would spawn another Job forever\n%v", args)
	}
}

// containerCommand pulls the /bin/sh wrapper out of the rendered Job.
func containerCommand(t *testing.T, container map[string]any) []string {
	t.Helper()
	command, ok := container["command"].([]string)
	if !ok {
		t.Fatalf("container command is not []string: %#v", container["command"])
	}
	return command
}

// TestInstallerJobCommandProbesForLocalSupport covers the image/CLI skew guard.
// A published dev tag gets rebuilt in place, so an image can report the same
// version as the CLI that launched it and still lack --local; the container
// then dies with a bare "unknown flag: --local". The preamble must therefore
// probe the binary's advertised flags, not its version string.
func TestInstallerJobCommandProbesForLocalSupport(t *testing.T) {
	_, container := jobSpec(t, installerJobManifest(newTestInClusterOptions(scimInstallConfig())))
	command := containerCommand(t, container)

	if len(command) != 4 {
		t.Fatalf("command = %v, want [/bin/sh -ec <script> <argv0>]", command)
	}
	if command[0] != "/bin/sh" || command[1] != "-ec" {
		t.Fatalf("command must be a /bin/sh -ec wrapper, got %v", command[:2])
	}

	script := command[2]
	for _, want := range []string{
		"est install --help", // capability probe, not a version comparison
		"--local",
		`exec est "$@"`,
		version.Version, // the version the launching CLI reports
		"est version",   // the version the image reports
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("preamble script is missing %q:\n%s", want, script)
		}
	}
	// The message must not claim the versions differ — in the real skew case they
	// are the identical string.
	if !strings.Contains(script, "OLDER BUILD") {
		t.Fatalf("preamble must describe the image as an older BUILD, not a different version:\n%s", script)
	}

	// The args are unchanged by the wrapper: still the recursion-safe invocation.
	args := container["args"].([]string)
	if len(args) < 3 || args[0] != "install" || args[1] != "--non-interactive" || args[2] != "--local" {
		t.Fatalf("args must still start with install --non-interactive --local, got %v", args)
	}
}

// TestInstallerJobCommandArgvHandling pins the subtlest part of the wrapper.
// Kubernetes runs command+args concatenated, and `sh -c <script>` takes the
// first operand after the script as $0. Without the placeholder in command[3],
// "install" would become $0 and vanish from "$@" — the install would run as
// `est --non-interactive --local …` and fail obscurely.
func TestInstallerJobCommandArgvHandling(t *testing.T) {
	_, container := jobSpec(t, installerJobManifest(newTestInClusterOptions(scimInstallConfig())))
	command := containerCommand(t, container)
	args := container["args"].([]string)

	argv0 := command[3]
	if strings.HasPrefix(argv0, "-") {
		t.Fatalf("the $0 placeholder %q looks like a flag; sh would treat it as an option", argv0)
	}
	if argv0 == "" || argv0 == args[0] {
		t.Fatalf("the $0 placeholder must be a distinct non-empty token, got %q", argv0)
	}

	// Prove it against a real shell rather than by reasoning: run the same argv
	// shape the kubelet builds, with a script that just reports "$@".
	if runtime.GOOS == "windows" {
		t.Skip("no /bin/sh")
	}
	shellArgs := append([]string{"-ec", `printf '%s\n' "$@"`, argv0}, args...)
	out, err := exec.Command("/bin/sh", shellArgs...).Output()
	if err != nil {
		t.Fatalf("probing argv handling: %v", err)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(got) != len(args) {
		t.Fatalf(`"$@" expanded to %d words, want %d (args = %v, got = %v)`, len(got), len(args), args, got)
	}
	for i := range args {
		if got[i] != args[i] {
			t.Fatalf(`"$@"[%d] = %q, want %q`, i, got[i], args[i])
		}
	}
}

// TestInstallerJobPreambleRunsUnderSh executes the real preamble against stub
// `est` binaries: one that advertises --local, one that does not. A syntax error
// or a broken probe here only shows up as a dead pod on a customer cluster.
func TestInstallerJobPreambleRunsUnderSh(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no /bin/sh")
	}
	_, container := jobSpec(t, installerJobManifest(newTestInClusterOptions(scimInstallConfig())))
	command := containerCommand(t, container)
	script, argv0 := command[2], command[3]
	args := container["args"].([]string)

	// `sh -n` first: parse-only, so a syntax error is reported as such.
	scriptPath := filepath.Join(t.TempDir(), "preamble.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("/bin/sh", "-n", scriptPath).CombinedOutput(); err != nil {
		t.Fatalf("sh -n rejected the preamble: %v\n%s\n---\n%s", err, out, script)
	}

	// stubEst writes a fake `est` whose behaviour follows from its own --help:
	// an install carrying a flag the help does not advertise fails the way cobra
	// fails, with a bare "unknown flag". That is the failure this preamble exists
	// to pre-empt, so the stub reproduces it rather than asserting around it.
	stubEst := func(t *testing.T, help string) string {
		t.Helper()
		dir := t.TempDir()
		body := "#!/bin/sh\n" +
			"HELP='" + help + "'\n" +
			`if [ "$1" = "install" ] && [ "$2" = "--help" ]; then printf '%s\n' "$HELP"; exit 0; fi` + "\n" +
			`if [ "$1" = "version" ]; then echo "0.1.4-dev (commit stale, built 2026-07-01)"; exit 0; fi` + "\n" +
			`if [ "$1" != "install" ]; then echo "unknown command: $1" >&2; exit 1; fi` + "\n" +
			`for flag in "$@"; do` + "\n" +
			`  if [ "$flag" = "--local" ] && ! printf '%s' "$HELP" | grep -q -- '--local'; then` + "\n" +
			`    echo "unknown flag: --local" >&2; exit 1` + "\n" +
			`  fi` + "\n" +
			`done` + "\n" +
			`echo "INSTALL RAN: $*"` + "\n"
		if err := os.WriteFile(filepath.Join(dir, "est"), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	run := func(t *testing.T, binDir string) (string, string, error) {
		t.Helper()
		cmd := exec.Command("/bin/sh", append([]string{"-ec", script, argv0}, args...)...)
		cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
		var stdout, stderr strings.Builder
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String(), err
	}

	t.Run("capable image runs the install", func(t *testing.T) {
		// The stub's fallback branch echoes the real-world failure, so if the probe
		// wrongly rejected a good image we would see "unknown flag" here.
		stdout, stderr, err := run(t, stubEst(t, `      --local   force the on-this-machine path`))
		if err != nil {
			t.Fatalf("preamble refused an image that supports --local: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}
		if strings.Contains(stderr, "FATAL") {
			t.Fatalf("preamble printed the skew error for a capable image:\n%s", stderr)
		}
	})

	t.Run("stale image fails with a diagnosis", func(t *testing.T) {
		_, stderr, err := run(t, stubEst(t, `      --non-interactive   no prompts`))
		if err == nil {
			t.Fatalf("preamble accepted an image with no --local; the pod would die on 'unknown flag'\n%s", stderr)
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			t.Fatalf("want exit status 1, got %v", err)
		}
		for _, want := range []string{
			"OLDER BUILD",
			"0.1.4-dev (commit stale, built 2026-07-01)", // what the image reports
			version.Version,     // what the CLI expected
			"--installer-image", // the fix
		} {
			if !strings.Contains(stderr, want) {
				t.Fatalf("skew error is missing %q:\n%s", want, stderr)
			}
		}
	})
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
