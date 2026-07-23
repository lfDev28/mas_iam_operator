package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	executil "github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/exec"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/ui"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/version"
)

const supportBundleTimeFormat = "20060102-150405"

type supportBundleOptions struct {
	namespace string
	outputDir string
	tail      int
}

type supportBundleCollector struct {
	ctx        context.Context
	runner     *executil.Runner
	client     *oc.Client
	namespace  string
	bundleDir  string
	cluster    clusterContext
	redactor   *supportRedactor
	collected  int
	warned     int
	startedAt  time.Time
	finishedAt time.Time
}

type secretSummary struct {
	Name string
	Type string
	Keys []string
}

func newSupportBundleCommand() *cobra.Command {
	opts := &supportBundleOptions{
		namespace: config.LoadInstallConfigFromEnv().Namespace,
		outputDir: ".",
		tail:      400,
	}

	command := &cobra.Command{
		Use:   "support-bundle",
		Short: "Collect a redacted MAS EST support evidence bundle",
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.run(cmd.Context())
		},
	}

	flags := command.Flags()
	flags.StringVar(&opts.namespace, "namespace", opts.namespace, "Target namespace")
	flags.StringVar(&opts.outputDir, "output-dir", opts.outputDir, "Directory where the bundle directory should be created")
	flags.IntVar(&opts.tail, "tail", opts.tail, "Number of log lines to collect per component")

	return command
}

func (o *supportBundleOptions) run(ctx context.Context) error {
	runner := executil.NewRunner()
	client := oc.NewClient(runner)
	cluster, err := ensureClusterLogin(ctx, runner, client, ui.IsInteractive())
	if err != nil {
		return err
	}

	namespace := strings.TrimSpace(o.namespace)
	if namespace == "" {
		namespace = config.DefaultNamespace
	}

	bundleDir, err := createSupportBundleDir(o.outputDir, namespace, time.Now())
	if err != nil {
		return err
	}

	collector := &supportBundleCollector{
		ctx:       ctx,
		runner:    runner,
		client:    client,
		namespace: namespace,
		bundleDir: bundleDir,
		cluster:   cluster,
		redactor:  newSupportRedactor(nil),
		startedAt: time.Now(),
	}

	fmt.Fprintf(os.Stdout, "[support-bundle] writing bundle to %s\n", bundleDir)
	if err := collector.collect(o.tail); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "[result] support bundle: %s\n", bundleDir)
	if collector.warned > 0 {
		fmt.Fprintf(os.Stdout, "[result] completed with %d collection warnings; see files ending in .error.txt\n", collector.warned)
	}
	return nil
}

func (c *supportBundleCollector) collect(tail int) error {
	c.writeText("mas-est-version.txt", version.String()+"\n")
	c.writeText("oc-whoami.txt", c.cluster.User+"\n")
	c.writeText("oc-server.txt", c.cluster.Server+"\n")

	secretJSON, secretErr := c.commandOutput([]string{"get", "secret", "-n", c.namespace, "-o", "json"})
	if secretErr != nil {
		c.writeError("secrets-summary.txt", secretErr)
	} else {
		redactor, err := redactorFromSecretListJSON([]byte(secretJSON))
		if err != nil {
			c.writeError("secrets-summary.txt", err)
		} else {
			c.redactor = redactor
			c.writeText("secrets-summary.txt", redactedSecretSummaryFromJSON([]byte(secretJSON)))
		}
	}

	var status bytes.Buffer
	printMASIAMStatus(&status, c.ctx, c.client, c.namespace, c.cluster)
	c.writeText("mas-est-status.txt", c.redactor.Redact(status.String()))

	c.collectCommand("pods-wide.txt", []string{"get", "pods", "-n", c.namespace, "-o", "wide"})
	c.collectCommand("deployments-wide.txt", []string{"get", "deployments", "-n", c.namespace, "-o", "wide"})
	c.collectCommand("jobs-wide.txt", []string{"get", "jobs", "-n", c.namespace, "-o", "wide"})
	c.collectCommand("pvcs-wide.txt", []string{"get", "pvc", "-n", c.namespace, "-o", "wide"})
	c.collectCommand("routes-wide.txt", []string{"get", "routes", "-n", c.namespace, "-o", "wide"})
	c.collectCommand("events.txt", []string{"get", "events", "-n", c.namespace, "--sort-by=.lastTimestamp"})
	c.collectCommand("storageclasses.txt", []string{"get", "storageclass"})
	c.collectCommand("configmaps.yaml", []string{"get", "configmap", "-n", c.namespace, "-o", "yaml"})

	for _, target := range []struct {
		file string
		args []string
	}{
		{
			file: "logs-operator.txt",
			args: []string{"logs", "deployment/mas-iam-operator-controller-manager", "-n", c.namespace, fmt.Sprintf("--tail=%d", tail)},
		},
		{
			file: "logs-keycloak.txt",
			args: []string{"logs", "deployment/mas-est-iam", "-n", c.namespace, fmt.Sprintf("--tail=%d", tail)},
		},
		{
			file: "logs-scim-bridge.txt",
			args: []string{"logs", "deployment/scim-bridge", "-n", c.namespace, fmt.Sprintf("--tail=%d", tail)},
		},
		{
			file: "logs-profile-bootstrap.txt",
			args: []string{"logs", "job/scim-bridge-mas-profile-bootstrap", "-n", c.namespace, fmt.Sprintf("--tail=%d", tail)},
		},
	} {
		c.collectCommand(target.file, target.args)
	}

	c.finishedAt = time.Now()
	c.writeManifest()
	return nil
}

func (c *supportBundleCollector) collectCommand(name string, args []string) {
	output, err := c.commandOutput(args)
	if err != nil {
		c.writeError(name, err)
		return
	}
	c.writeText(name, c.redactor.Redact(output))
}

func (c *supportBundleCollector) commandOutput(args []string) (string, error) {
	return c.runner.Output(c.ctx, executil.Options{Name: "oc", Args: args})
}

func (c *supportBundleCollector) writeText(name, value string) {
	path := filepath.Join(c.bundleDir, name)
	if !strings.HasSuffix(value, "\n") {
		value += "\n"
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "[warn] unable to write %s: %v\n", path, err)
		c.warned++
		return
	}
	c.collected++
}

func (c *supportBundleCollector) writeError(name string, err error) {
	errorName := strings.TrimSuffix(name, filepath.Ext(name)) + ".error.txt"
	c.writeText(errorName, fmt.Sprintf("collection failed for %s: %v\n", name, err))
	c.warned++
}

func (c *supportBundleCollector) writeManifest() {
	finished := c.finishedAt
	if finished.IsZero() {
		finished = time.Now()
	}
	manifest := fmt.Sprintf(`MAS External Services Toolkit support bundle
Namespace: %s
Cluster user: %s
Cluster server: %s
Started: %s
Finished: %s
Files written: %d
Collection warnings: %d

Redaction:
- Secret values are not written to secrets-summary.txt.
- Known secret-derived sensitive values are redacted from collected text.
- Review hostnames, customer identifiers, and environment-specific resource names before sharing externally.
`, c.namespace, c.cluster.User, c.cluster.Server, c.startedAt.Format(time.RFC3339), finished.Format(time.RFC3339), c.collected, c.warned)
	c.writeText("README.txt", manifest)
}

func createSupportBundleDir(outputDir, namespace string, now time.Time) (string, error) {
	base := strings.TrimSpace(outputDir)
	if base == "" {
		base = "."
	}
	name := fmt.Sprintf("mas-est-support-%s-%s", safePathToken(namespace), now.Format(supportBundleTimeFormat))
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", fmt.Errorf("create support bundle parent directory %s: %w", base, err)
	}
	for i := 0; i < 100; i++ {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s-%02d", name, i)
		}
		path := filepath.Join(base, candidate)
		if err := os.Mkdir(path, 0o700); err == nil {
			abs, absErr := filepath.Abs(path)
			if absErr != nil {
				return path, nil
			}
			return abs, nil
		} else if !os.IsExist(err) {
			return "", fmt.Errorf("create support bundle directory %s: %w", path, err)
		}
	}
	return "", fmt.Errorf("create support bundle directory: too many existing directories for %s", name)
}

func safePathToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "namespace"
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func redactedSecretSummaryFromJSON(raw []byte) string {
	summaries, err := parseSecretSummaries(raw)
	if err != nil {
		return fmt.Sprintf("unable to parse secret list: %v\n", err)
	}
	if len(summaries) == 0 {
		return "No secrets found.\n"
	}

	var b strings.Builder
	b.WriteString("Secret summaries; values intentionally omitted.\n\n")
	for _, summary := range summaries {
		fmt.Fprintf(&b, "secret/%s\n", summary.Name)
		fmt.Fprintf(&b, "type: %s\n", summary.Type)
		if len(summary.Keys) == 0 {
			b.WriteString("keys: none\n\n")
			continue
		}
		b.WriteString("keys:\n")
		for _, key := range summary.Keys {
			fmt.Fprintf(&b, "- %s\n", key)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func parseSecretSummaries(raw []byte) ([]secretSummary, error) {
	var payload struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Type string            `json:"type"`
			Data map[string]string `json:"data"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode secret list: %w", err)
	}

	summaries := make([]secretSummary, 0, len(payload.Items))
	for _, item := range payload.Items {
		keys := make([]string, 0, len(item.Data))
		for key := range item.Data {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		summaries = append(summaries, secretSummary{
			Name: item.Metadata.Name,
			Type: item.Type,
			Keys: keys,
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Name < summaries[j].Name
	})
	return summaries, nil
}

func redactorFromSecretListJSON(raw []byte) (*supportRedactor, error) {
	var payload struct {
		Items []struct {
			Data map[string]string `json:"data"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode secret list for redaction: %w", err)
	}

	values := []string{}
	for _, item := range payload.Items {
		for key, encoded := range item.Data {
			if !isSensitiveSecretKey(key) {
				continue
			}
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return nil, fmt.Errorf("decode secret key %s for redaction: %w", key, err)
			}
			value := strings.TrimSpace(string(decoded))
			if len(value) >= 4 {
				values = append(values, value)
			}
		}
	}
	return newSupportRedactor(values), nil
}

func isSensitiveSecretKey(key string) bool {
	normalized := strings.ToLower(key)
	for _, marker := range []string{"token", "secret", "password", "passwd", "credential", "apikey", "api_key", "api-token", "api_token"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

type supportRedactor struct {
	values        []string
	authorization *regexp.Regexp
	assignments   *regexp.Regexp
}

func newSupportRedactor(values []string) *supportRedactor {
	unique := map[string]struct{}{}
	cleaned := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if len(value) < 4 {
			continue
		}
		if _, ok := unique[value]; ok {
			continue
		}
		unique[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	sort.Slice(cleaned, func(i, j int) bool {
		return len(cleaned[i]) > len(cleaned[j])
	})

	return &supportRedactor{
		values:        cleaned,
		authorization: regexp.MustCompile(`(?i)(authorization:\s*(?:bearer|basic)\s+)[^\s]+`),
		assignments:   regexp.MustCompile(`(?i)((?:token|password|passwd|secret|client_secret|api[_-]?key)\s*[:=]\s*)("[^"\s]+"|'[^'\s]+'|[^\s]+)`),
	}
}

func (r *supportRedactor) Redact(value string) string {
	if r == nil {
		return value
	}
	for _, secretValue := range r.values {
		value = strings.ReplaceAll(value, secretValue, "[REDACTED]")
	}
	value = r.authorization.ReplaceAllString(value, "${1}[REDACTED]")
	value = r.assignments.ReplaceAllString(value, "${1}[REDACTED]")
	return value
}
