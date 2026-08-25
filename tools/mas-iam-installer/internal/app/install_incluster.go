package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/ui"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/version"
)

const (
	installerJobDefaultName     = "mas-est-install"
	installerServiceAccountName = "mas-est-installer"
	installerRBACName           = "mas-est-installer"
	installerTokenSecretName    = "mas-est-install-credentials"
	installerImageRepository    = "quay.io/lee_forster/mas-external-services-tool"

	// 90 minutes. Clear of the slowest observed run (MAS 9.1.4 reconciles each
	// IDPCfg at ~7 min) while still bounding a hang, per DESIGN-IN-CLUSTER-INSTALLER.
	installerJobActiveDeadlineSeconds = 5400

	// How long to keep retrying the log attach before the first byte arrives.
	// Covers image pull onto a cold node plus scheduling.
	installerLogAttachTimeout = 15 * time.Minute

	// Grace period for the Job's status to reach a terminal condition after
	// the log stream ends. The controller writes it within a second or two.
	installerJobSettleTimeout = 2 * time.Minute
)

// defaultInstallerImage pins the Job to the CLI's own version so the in-cluster
// run is the same build as the laptop run that launched it.
func defaultInstallerImage() string {
	return installerImageRepository + ":v" + version.Version
}

type inClusterInstallOptions struct {
	cfg         config.InstallConfig
	jobName     string
	image       string
	interactive bool
	out         io.Writer
}

func (o *inClusterInstallOptions) run(ctx context.Context, client *oc.Client) error {
	if o.out == nil {
		o.out = os.Stdout
	}
	if strings.TrimSpace(o.jobName) == "" {
		o.jobName = installerJobDefaultName
	}
	if strings.TrimSpace(o.image) == "" {
		o.image = defaultInstallerImage()
	}

	if err := o.reconcileExistingJob(ctx, client); err != nil {
		return err
	}

	for _, manifest := range o.manifests() {
		if err := applyManifest(ctx, client, manifest); err != nil {
			return err
		}
	}

	fmt.Fprintf(o.out, "\n[in-cluster] launched job/%s in namespace %s (image %s)\n", o.jobName, o.cfg.Namespace, o.image)
	fmt.Fprintln(o.out, "[in-cluster] Ctrl-C detaches from the log stream; it does NOT cancel the install.")
	fmt.Fprintf(o.out, "[in-cluster] reattach with: mas-est logs --namespace %s --component install-job --follow\n", o.cfg.Namespace)
	fmt.Fprintf(o.out, "[in-cluster] cancel with:   oc delete job %s -n %s\n\n", o.jobName, o.cfg.Namespace)

	if err := o.streamJobLogs(ctx, client); err != nil {
		return err
	}

	status, err := o.waitForJobResult(ctx, client)
	if err != nil {
		return err
	}
	switch {
	case status.Complete:
		fmt.Fprintf(o.out, "\n[result] install job %s/%s completed\n", o.cfg.Namespace, o.jobName)
		fmt.Fprintf(o.out, "[result] connection details: mas-est details --namespace %s --component all\n", o.cfg.Namespace)
		return nil
	case status.FailedCondition:
		return fmt.Errorf("install job %s/%s failed (%s: %s); inspect with 'mas-est logs --namespace %s --component install-job'",
			o.cfg.Namespace, o.jobName, defaultString(status.FailedReason, "Failed"), status.FailedMessage, o.cfg.Namespace)
	default:
		return fmt.Errorf("install job %s/%s did not reach a terminal state within %s; reattach with 'mas-est logs --namespace %s --component install-job --follow'",
			o.cfg.Namespace, o.jobName, installerJobSettleTimeout, o.cfg.Namespace)
	}
}

// reconcileExistingJob refuses to silently replace a Job of the same name: a
// running one may be somebody else's install, and a finished one is the
// post-mortem artifact (no ttlSecondsAfterFinished by design).
func (o *inClusterInstallOptions) reconcileExistingJob(ctx context.Context, client *oc.Client) error {
	status, err := client.Job(ctx, o.cfg.Namespace, o.jobName)
	if err != nil {
		if oc.IsNotFound(err) {
			return nil
		}
		return err
	}

	state := "has already finished"
	if status.Active > 0 {
		state = "is still running"
	}
	summary := fmt.Sprintf("job/%s already exists in namespace %s and %s", o.jobName, o.cfg.Namespace, state)

	if !o.interactive {
		return fmt.Errorf("%s; delete it with 'oc delete job %s -n %s' or pass --job-name with a different name",
			summary, o.jobName, o.cfg.Namespace)
	}

	fmt.Fprintf(o.out, "[in-cluster] %s\n", summary)
	confirmed, err := ui.Confirm("Delete it and start a new install job?", false)
	if err != nil {
		return err
	}
	if !confirmed {
		return fmt.Errorf("%s; reattach with 'mas-est logs --namespace %s --component install-job --follow' or pass --job-name",
			summary, o.cfg.Namespace)
	}
	if _, err := client.DeleteIgnoreNotFound(ctx, o.cfg.Namespace, "job/"+o.jobName); err != nil {
		return err
	}
	return nil
}

// streamJobLogs follows the Job until the container exits. `oc logs -f` fails
// while the pod is still Pending/ContainerCreating, so the first attach is
// retried; once bytes have arrived a stream error means a dropped connection,
// and reattaching would replay the log from the top, so we stop and let the
// caller report the Job's real state.
func (o *inClusterInstallOptions) streamJobLogs(ctx context.Context, client *oc.Client) error {
	counter := &countingWriter{writer: o.out}
	deadline := time.Now().Add(installerLogAttachTimeout)

	for {
		err := client.Logs(ctx, o.cfg.Namespace, []string{"job/" + o.jobName, "-f"}, counter, os.Stderr)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if counter.written > 0 {
			fmt.Fprintf(os.Stderr, "[in-cluster] log stream ended early (%v); the install job keeps running\n", err)
			return nil
		}

		if status, statusErr := client.Job(ctx, o.cfg.Namespace, o.jobName); statusErr == nil {
			if status.Complete || status.FailedCondition {
				// Terminal before we ever attached (fast failure, or a pod that
				// never started). One non-follow read to surface whatever the
				// container did print.
				_ = client.Logs(ctx, o.cfg.Namespace, []string{"job/" + o.jobName}, counter, os.Stderr)
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("job/%s in namespace %s produced no logs within %s; check 'oc describe job %s -n %s': %w",
				o.jobName, o.cfg.Namespace, installerLogAttachTimeout, o.jobName, o.cfg.Namespace, err)
		}
		if err := sleepCtx(ctx, 3*time.Second); err != nil {
			return err
		}
	}
}

func (o *inClusterInstallOptions) waitForJobResult(ctx context.Context, client *oc.Client) (oc.JobStatus, error) {
	deadline := time.Now().Add(installerJobSettleTimeout)
	var last oc.JobStatus
	for {
		status, err := client.Job(ctx, o.cfg.Namespace, o.jobName)
		if err == nil {
			last = status
			if status.Complete || status.FailedCondition {
				return status, nil
			}
		}
		if time.Now().After(deadline) {
			return last, nil
		}
		if err := sleepCtx(ctx, 3*time.Second); err != nil {
			return last, err
		}
	}
}

type countingWriter struct {
	writer  io.Writer
	written int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.writer.Write(p)
	c.written += int64(n)
	return n, err
}

func (o *inClusterInstallOptions) manifests() []map[string]any {
	manifests := []map[string]any{
		installerNamespaceManifest(o.cfg.Namespace),
		installerServiceAccountManifest(o.cfg.Namespace),
		installerClusterRoleManifest(),
		installerClusterRoleBindingManifest(o.cfg.Namespace),
		installerRoleManifest(o.cfg.Namespace),
		installerRoleBindingManifest(o.cfg.Namespace),
	}
	if data := installerSecretData(o.cfg); len(data) > 0 {
		manifests = append(manifests, installerSecretManifest(o.cfg.Namespace, data))
	}
	return append(manifests, installerJobManifest(o))
}

func installerLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/component":  "installer",
		"app.kubernetes.io/managed-by": "mas-est-installer",
		"app.kubernetes.io/name":       "mas-est",
	}
}

func installerNamespaceManifest(namespace string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name":   namespace,
			"labels": installerLabels(),
		},
	}
}

func installerServiceAccountManifest(namespace string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "ServiceAccount",
		"metadata": map[string]any{
			"name":      installerServiceAccountName,
			"namespace": namespace,
			"labels":    installerLabels(),
		},
	}
}

func rbacRule(apiGroups, resources, verbs []string) map[string]any {
	return map[string]any{
		"apiGroups": apiGroups,
		"resources": resources,
		"verbs":     verbs,
	}
}

// installerClusterRoleManifest holds everything the install touches outside its
// own namespace. Cluster-scoped because the MAS core namespace and the MAS
// Mongo namespace are not known until the config is resolved and vary per MAS
// instance. pods/exec plus cross-namespace secret reads make this close to
// cluster-admin in practice — documented in docs/INSTALL-ALL-IN-ONE.md so
// operators can substitute a narrower role.
func installerClusterRoleManifest() map[string]any {
	rules := []map[string]any{
		// Target namespace creation, and the MasIamStack CRD that
		// manifests/install-olm.yaml applies ahead of the OLM Subscription.
		// update/patch as well as create throughout: the scripts use `oc apply`,
		// which patches an existing object and writes the
		// kubectl.kubernetes.io/last-applied-configuration annotation. Any
		// resource that survives a previous install (the CRD and the SCC both
		// do — neither is namespaced) is a patch, not a create.
		rbacRule([]string{""}, []string{"namespaces"}, []string{"get", "list", "create", "update", "patch"}),
		rbacRule([]string{"apiextensions.k8s.io"}, []string{"customresourcedefinitions"},
			[]string{"get", "list", "create", "update", "patch"}),
		rbacRule([]string{"storage.k8s.io"}, []string{"storageclasses"}, []string{"get", "list"}),
		// Node architecture drives the operator/catalog image variant chosen by
		// scripts/_all_in_one_common.sh.
		rbacRule([]string{""}, []string{"nodes"}, []string{"get", "list"}),
		// The S3 and SMTP components derive their route hosts from the default
		// IngressController's domain (defaultObjectStorageRouteHost, used by
		// both object_storage.go and smtp.go) whenever --route-host is not
		// given. It lives in openshift-ingress-operator, so it needs a
		// cluster-scoped read.
		rbacRule([]string{"operator.openshift.io"}, []string{"ingresscontrollers"}, []string{"get", "list"}),

		// anyuid for the OpenLDAP TLS generator job. Held by the installer SA
		// itself so the RBAC escalation check lets it apply the anyuid
		// ClusterRoleBinding in manifests/install-olm.yaml onward to the
		// operator's service account.
		{
			"apiGroups":     []string{"security.openshift.io"},
			"resources":     []string{"securitycontextconstraints"},
			"resourceNames": []string{"anyuid"},
			"verbs":         []string{"use"},
		},
		// manifests/install-olm-sample.yaml applies its own SCC
		// (mas-est-iam-openldap-tls-generator), which is cluster-scoped and so
		// outlives the namespace — every reinstall re-applies it.
		rbacRule([]string{"security.openshift.io"}, []string{"securitycontextconstraints"},
			[]string{"get", "list", "create", "update", "patch"}),
		// `bind` is the narrow escape hatch from RBAC escalation prevention:
		// it lets the installer create the two bindings in install-olm.yaml
		// without holding every permission those roles carry.
		{
			"apiGroups":     []string{"rbac.authorization.k8s.io"},
			"resources":     []string{"clusterroles"},
			"resourceNames": []string{"admin", "system:openshift:scc:anyuid"},
			"verbs":         []string{"bind"},
		},
		rbacRule([]string{"rbac.authorization.k8s.io"}, []string{"clusterroles", "clusterrolebindings"},
			[]string{"get", "list", "create", "update", "patch"}),

		// OLM. CatalogSource lives in openshift-marketplace, the
		// OperatorGroup/Subscription/CSV in the target namespace.
		rbacRule([]string{"operators.coreos.com"},
			[]string{"catalogsources", "operatorgroups", "subscriptions", "clusterserviceversions", "installplans"},
			[]string{"get", "list", "watch", "create", "update", "patch", "delete"}),
		rbacRule([]string{"packages.operators.coreos.com"}, []string{"packagemanifests"}, []string{"get", "list"}),

		// MAS core namespace: IDPCfgs for LDAP/OIDC/SAML, ObjectStorageCfg for
		// the S3 component, the Suite CR podTemplates entitymgr memory bump,
		// the {instance}-selfreg ConfigMap, and the credential secrets those
		// configs reference.
		rbacRule([]string{"config.mas.ibm.com"}, []string{"idpcfgs", "objectstoragecfgs"},
			[]string{"get", "list", "create", "update", "patch"}),
		rbacRule([]string{"core.mas.ibm.com"}, []string{"suites"}, []string{"get", "list", "patch"}),
		rbacRule([]string{"apps.mas.ibm.com"}, []string{"manageworkspaces"}, []string{"get", "list"}),
		// update/patch too: the IDPCfg credential secrets in the MAS core
		// namespace are re-applied on every mas-auth run.
		rbacRule([]string{""}, []string{"secrets"}, []string{"get", "list", "create", "update", "patch"}),
		rbacRule([]string{""}, []string{"configmaps"}, []string{"get", "list", "create", "update", "patch", "delete"}),
		// Route reads are cluster-wide: MAS API/auth route discovery runs
		// `oc get route -A`.
		rbacRule([]string{"route.openshift.io"}, []string{"routes"}, []string{"get", "list"}),

		// pods/exec is required by scripts/link-scim-users-oidc.sh, which runs
		// `oc exec … mongosh` inside the MAS Mongo pod, and by the Keycloak
		// script bootstrap.
		rbacRule([]string{""}, []string{"pods"}, []string{"get", "list"}),
		rbacRule([]string{""}, []string{"pods/log"}, []string{"get"}),
		rbacRule([]string{""}, []string{"pods/exec"}, []string{"create"}),
	}

	return map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "ClusterRole",
		"metadata": map[string]any{
			"name":   installerRBACName,
			"labels": installerLabels(),
		},
		"rules": rules,
	}
}

// installerClusterRoleBindingManifest is namespace-suffixed so two engineers
// installing into different namespaces on one cluster do not fight over a
// single binding's subject list.
func installerClusterRoleBindingManifest(namespace string) map[string]any {
	return map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "ClusterRoleBinding",
		"metadata": map[string]any{
			"name":   installerRBACName + "-" + namespace,
			"labels": installerLabels(),
		},
		"roleRef": map[string]string{
			"apiGroup": "rbac.authorization.k8s.io",
			"kind":     "ClusterRole",
			"name":     installerRBACName,
		},
		"subjects": []map[string]string{{
			"kind":      "ServiceAccount",
			"name":      installerServiceAccountName,
			"namespace": namespace,
		}},
	}
}

// installerRoleManifest is the full-CRUD half, deliberately namespaced: the
// installer needs to delete and rewrite workloads only inside its own target
// namespace, so those verbs are not granted cluster-wide.
func installerRoleManifest(namespace string) map[string]any {
	crud := []string{"get", "list", "watch", "create", "update", "patch", "delete"}
	return map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "Role",
		"metadata": map[string]any{
			"name":      installerRBACName,
			"namespace": namespace,
			"labels":    installerLabels(),
		},
		"rules": []map[string]any{
			rbacRule([]string{""},
				[]string{"secrets", "configmaps", "services", "persistentvolumeclaims", "serviceaccounts", "pods"}, crud),
			rbacRule([]string{"apps"}, []string{"deployments", "statefulsets"}, crud),
			rbacRule([]string{"batch"}, []string{"jobs"}, crud),
			rbacRule([]string{"route.openshift.io"}, []string{"routes"}, crud),
			// OpenShift gates spec.host on a Route behind the routes/custom-host
			// subresource; without it every Route the installer creates with an
			// explicit host (MinIO api/console, Mailpit UI, Keycloak) is rejected
			// with "you do not have permission to set the host field of the
			// route". update as well as create, because re-applying an existing
			// Route with a host is checked the same way.
			rbacRule([]string{"route.openshift.io"}, []string{"routes/custom-host"},
				[]string{"create", "update"}),
			rbacRule([]string{"iam.mas.ibm.com"}, []string{"masiamstacks"}, crud),
			// manifests/install-olm.yaml binds the operator service account to
			// the admin ClusterRole in this namespace.
			rbacRule([]string{"rbac.authorization.k8s.io"}, []string{"roles", "rolebindings"},
				[]string{"get", "list", "create", "update", "patch"}),
		},
	}
}

func installerRoleBindingManifest(namespace string) map[string]any {
	return map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "RoleBinding",
		"metadata": map[string]any{
			"name":      installerRBACName,
			"namespace": namespace,
			"labels":    installerLabels(),
		},
		"roleRef": map[string]string{
			"apiGroup": "rbac.authorization.k8s.io",
			"kind":     "Role",
			"name":     installerRBACName,
		},
		"subjects": []map[string]string{{
			"kind":      "ServiceAccount",
			"name":      installerServiceAccountName,
			"namespace": namespace,
		}},
	}
}

// installerSecretData carries every value that must not appear in the Job spec.
// Both the MAS API token and the SMTP relay password would otherwise be plain
// text in `oc get job -o yaml`. The keys are the env var names the CLI already
// reads in config.LoadInstallConfigFromEnv.
func installerSecretData(cfg config.InstallConfig) map[string]string {
	data := map[string]string{}
	if value := strings.TrimSpace(cfg.MASAPITokenName); value != "" {
		data[config.EnvMASAPITokenName] = value
	}
	if value := strings.TrimSpace(cfg.MASAPITokenValue); value != "" {
		data[config.EnvMASAPITokenValue] = value
	}
	if cfg.SMTPRelayPassword != "" {
		data[config.EnvSMTPRelayPassword] = cfg.SMTPRelayPassword
	}
	return data
}

func installerSecretManifest(namespace string, data map[string]string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"type":       "Opaque",
		"metadata": map[string]any{
			"name":      installerTokenSecretName,
			"namespace": namespace,
			"labels":    installerLabels(),
		},
		"stringData": data,
	}
}

func installerJobManifest(o *inClusterInstallOptions) map[string]any {
	env := []map[string]any{{
		// The image runs under an arbitrary UID on clusters that do not grant
		// anyuid, where $HOME is unwritable and oc's discovery cache and the
		// bash helpers both want somewhere to write.
		"name":  "HOME",
		"value": "/tmp",
	}}
	for _, key := range sortedKeys(installerSecretData(o.cfg)) {
		env = append(env, map[string]any{
			"name": key,
			"valueFrom": map[string]any{
				"secretKeyRef": map[string]string{
					"name": installerTokenSecretName,
					"key":  key,
				},
			},
		})
	}

	return map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]any{
			"name":      o.jobName,
			"namespace": o.cfg.Namespace,
			"labels":    installerLabels(),
		},
		"spec": map[string]any{
			// backoffLimit 0 + restartPolicy Never: the install is idempotent,
			// but a blind restart silently repeats 20+ minutes of work. A human
			// deciding to re-run beats a controller doing it.
			"backoffLimit":          0,
			"activeDeadlineSeconds": installerJobActiveDeadlineSeconds,
			// No ttlSecondsAfterFinished: the finished Job and its logs are the
			// post-mortem artifact.
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": installerLabels(),
				},
				"spec": map[string]any{
					"restartPolicy":      "Never",
					"serviceAccountName": installerServiceAccountName,
					"containers": []map[string]any{{
						"name":    "installer",
						"image":   o.image,
						"command": installerJobCommand(),
						"args":    installerJobArgs(o.cfg),
						"env":     env,
					}},
				},
			},
		},
	}
}

// installerJobPreamble is the /bin/sh wrapper the Job container runs before the
// real install. Its whole job is to turn one specific, baffling failure into a
// readable one.
//
// The Job's args always carry --local (see installerJobArgs). An installer image
// whose `est` predates that flag dies instantly with a bare
// `unknown flag: --local` and nothing else, which reads as a broken install
// rather than as version skew. Worse, the versions cannot be compared: a dev tag
// like v0.1.4-dev gets rebuilt and re-pushed in place, so a stale image reports
// the exact same version string as the CLI that launched it. So the probe tests
// the image's CAPABILITY — does its `est install --help` advertise --local — not
// its version.
//
// %s is version.Version, substituted at Job-build time so the message can name
// what the launching CLI was. The script contains no other %-verbs.
const installerJobPreamble = `# Capability probe: does this image's est understand --local? Version strings
# cannot answer that (a dev tag is rebuilt in place), so ask the binary.
if ! est install --help 2>/dev/null | grep -q -- '--local'; then
cat >&2 <<EOF
[mas-est] FATAL: installer image / CLI skew - refusing to run.

  This image's 'est install' does not understand --local, so the image is an
  OLDER BUILD than the mas-est CLI that created this Job. Version strings do not
  settle this: a dev tag can be rebuilt and re-pushed in place, so the same tag
  and the same reported version can be two different builds.

    image reports: $(est version 2>&1 || echo unknown)
    CLI expected:  %s

  Without --local the in-cluster install would spawn installer Jobs without
  bound, so this container stops here instead.

  Fix: delete this Job, then re-run 'mas-est install' either with
  --installer-image pointing at an image built from this CLI, or after
  rebuilding and re-pushing the tag above (use a fresh tag, or force the node to
  re-pull, so the stale layer is not reused).
EOF
exit 1
fi
exec est "$@"
`

// installerJobCommand overrides the image's ENTRYPOINT ["est"] with the shell
// preamble above, so `est` must be invoked explicitly.
//
// argv: Kubernetes concatenates command + args, giving
// `/bin/sh -ec <script> mas-est-install install --non-interactive --local …`.
// `sh -c` assigns the first operand after the script to $0 and the rest to
// $1…$n, so the "mas-est-install" placeholder is what keeps "$@" equal to the
// full args list. Drop it and `install` would be swallowed as $0. Guarded by
// TestInstallerJobCommandArgvHandling.
func installerJobCommand() []string {
	return []string{
		"/bin/sh",
		"-ec",
		fmt.Sprintf(installerJobPreamble, version.Version),
		installerJobArgv0,
	}
}

// installerJobArgv0 is the $0 placeholder for `sh -c`; it must not look like a
// flag, and it shows up as the program name in any shell diagnostic.
const installerJobArgv0 = "mas-est-install"

// installerJobArgs renders the already-resolved config as explicit `est install`
// flags, so `oc get job -o yaml` documents exactly what was requested. The MAS
// API token and the SMTP relay password are deliberately absent — they arrive
// through secretKeyRef env vars instead.
//
// --local is MANDATORY and must never be removed. The container runs this same
// CLI, where --in-cluster now DEFAULTS TO TRUE. Without --local the Job's own
// `est install` would create a second installer Job, which would create a
// third, and so on without bound — the cluster would fill with installer Jobs
// and no install would ever run. Guarded by
// TestInstallerJobArgsAlwaysPassesLocal.
func installerJobArgs(cfg config.InstallConfig) []string {
	args := []string{"install", "--non-interactive", "--local"}
	add := func(flag, value string) {
		if strings.TrimSpace(value) != "" {
			args = append(args, flag, value)
		}
	}

	add("--namespace", cfg.Namespace)
	add("--components", config.InstallComponentsString(cfg.Components))
	add("--mas-instance-id", cfg.MASInstanceID)
	add("--mas-core-namespace", cfg.MASCoreNamespace)
	add("--storage-class", cfg.StorageClass)
	add("--keycloak-bootstrap", cfg.KeycloakBootstrapMethod)
	add("--idpcfg-memory-limit", cfg.IDPCfgMemoryLimit)

	if cfg.HasComponent(config.InstallComponentSCIM) {
		add("--mas-base-url", cfg.MASBaseURL)
		add("--workspace-id", cfg.WorkspaceID)
		add("--profile-id", cfg.ProfileID)
		add("--scim-bridge-storage-class", cfg.ScimBridgeStorageClass)
	}

	if cfg.HasComponent(config.InstallComponentS3) && cfg.SkipS3MASConfig {
		args = append(args, "--skip-s3-mas-config")
	}

	if cfg.HasComponent(config.InstallComponentSMTP) && strings.TrimSpace(cfg.SMTPRelayHost) != "" {
		add("--smtp-relay-host", cfg.SMTPRelayHost)
		if cfg.SMTPRelayPort > 0 {
			args = append(args, "--smtp-relay-port", strconv.Itoa(cfg.SMTPRelayPort))
		}
		add("--smtp-relay-username", cfg.SMTPRelayUsername)
		add("--smtp-relay-from", cfg.SMTPRelayFrom)
		add("--smtp-relay-auth", cfg.SMTPRelayAuth)
		add("--smtp-relay-allowed-recipients", cfg.SMTPRelayAllowedRcpt)
		args = append(args, fmt.Sprintf("--smtp-relay-starttls=%t", cfg.SMTPRelayStartTLS))
	}

	if cfg.ConfigureMASAuth {
		args = append(args, "--configure-mas-auth")
		add("--mas-auth-providers", config.MASAuthProvidersString(cfg.MASAuthProviders))
		add("--mas-auth-host", cfg.MASAuthHost)
		add("--mas-auth-instance-id", cfg.MASAuthInstanceID)
		add("--mas-auth-core-namespace", cfg.MASAuthCoreNamespace)
		if cfg.MASAuthUseCRApply {
			args = append(args, "--mas-auth-use-cr-apply")
		}
	}

	return args
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
