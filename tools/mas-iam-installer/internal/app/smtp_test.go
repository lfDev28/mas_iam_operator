package app

import (
	"strings"
	"testing"
)

func TestMailpitEndpoints(t *testing.T) {
	opts := &mailpitInstallOptions{
		namespace: "mas-est",
		name:      "mas-mailpit",
	}

	if got := opts.serviceHost(); got != "mas-mailpit.mas-est.svc.cluster.local" {
		t.Fatalf("serviceHost() = %q", got)
	}
	if got := opts.smtpURL(); got != "smtp://mas-mailpit.mas-est.svc.cluster.local:1025" {
		t.Fatalf("smtpURL() = %q", got)
	}
}

func TestMailpitServiceManifest(t *testing.T) {
	opts := &mailpitInstallOptions{
		namespace: "mas-est",
		name:      "mas-mailpit",
	}

	manifest := mailpitServiceManifest(opts)
	metadata := manifest["metadata"].(map[string]any)
	if metadata["name"] != "mas-mailpit" {
		t.Fatalf("service name = %q", metadata["name"])
	}
	spec := manifest["spec"].(map[string]any)
	ports := spec["ports"].([]map[string]any)
	if len(ports) != 2 {
		t.Fatalf("ports = %v, want smtp and ui", ports)
	}
	if ports[0]["name"] != "smtp" || ports[0]["port"] != defaultMailpitSMTPPort || ports[0]["targetPort"] != "smtp" {
		t.Fatalf("smtp port = %v", ports[0])
	}
	if ports[1]["name"] != "ui" || ports[1]["port"] != defaultMailpitUIPort || ports[1]["targetPort"] != "ui" {
		t.Fatalf("ui port = %v", ports[1])
	}
}

func TestMailpitDeploymentManifest(t *testing.T) {
	opts := &mailpitInstallOptions{
		namespace: "mas-est",
		name:      "mas-mailpit",
		image:     "docker.io/axllent/mailpit:latest",
	}

	manifest := mailpitDeploymentManifest(opts)
	spec := manifest["spec"].(map[string]any)
	template := spec["template"].(map[string]any)
	podSpec := template["spec"].(map[string]any)
	containers := podSpec["containers"].([]map[string]any)
	container := containers[0]
	if container["image"] != "docker.io/axllent/mailpit:latest" {
		t.Fatalf("image = %q", container["image"])
	}
	ports := container["ports"].([]map[string]any)
	if ports[0]["containerPort"] != defaultMailpitSMTPPort {
		t.Fatalf("smtp container port = %v", ports[0])
	}
	if ports[1]["containerPort"] != defaultMailpitUIPort {
		t.Fatalf("ui container port = %v", ports[1])
	}
}

func TestSMTPConnectionConfigMapData(t *testing.T) {
	opts := &mailpitInstallOptions{
		namespace: "mas-est",
		name:      "mas-mailpit",
		routeHost: "mas-mailpit.apps.example.com",
	}

	data := opts.smtpConnectionConfigMapData()
	wants := map[string]string{
		"host":           "mas-mailpit.mas-est.svc.cluster.local",
		"port":           "1025",
		"from":           "mas-est@example.local",
		"webUI":          "https://mas-mailpit.apps.example.com",
		"tls":            "disabled",
		"authentication": "none",
		"relayEnabled":   "false",
	}
	for k, want := range wants {
		if data[k] != want {
			t.Fatalf("data[%q] = %q, want %q", k, data[k], want)
		}
	}
	if _, present := data["relayHost"]; present {
		t.Fatalf("relayHost should be absent when relay disabled, got %q", data["relayHost"])
	}
}

func TestSMTPConnectionConfigMapDataWithRelay(t *testing.T) {
	opts := &mailpitInstallOptions{
		namespace:     "mas-est",
		name:          "mas-mailpit",
		routeHost:     "mas-mailpit.apps.example.com",
		relayHost:     "smtp.gmail.com",
		relayPort:     587,
		relayUsername: "lab@example.com",
		relayPassword: "shh",
		relayFrom:     "lab@example.com",
		relayAuth:     "plain",
		relayStartTLS: true,
	}

	data := opts.smtpConnectionConfigMapData()
	wants := map[string]string{
		"relayEnabled":           "true",
		"relayHost":              "smtp.gmail.com",
		"relayPort":              "587",
		"relayFrom":              "lab@example.com",
		"relayStartTLS":          "true",
		"relayAuth":              "plain",
		"relayCredentialsSecret": "mas-est-mailpit-relay",
	}
	for k, want := range wants {
		if data[k] != want {
			t.Fatalf("data[%q] = %q, want %q", k, data[k], want)
		}
	}
	// The credentials must NEVER end up in the non-sensitive ConfigMap.
	for _, sensitive := range []string{"shh", "lab@example.com:shh"} {
		for k, v := range data {
			if strings.Contains(v, sensitive) && k != "relayUsername" && k != "relayFrom" {
				if k == "relayHost" || k == "relayPort" || k == "relayFrom" {
					continue
				}
				t.Fatalf("ConfigMap key %q leaks credential material: %q", k, v)
			}
		}
	}
	if _, present := data["relayPassword"]; present {
		t.Fatalf("relayPassword must not appear in non-sensitive ConfigMap")
	}
	if _, present := data["relayUsername"]; present {
		t.Fatalf("relayUsername must not appear in non-sensitive ConfigMap (only via Secret)")
	}
}

func TestMailpitRelayConfigYAMLRendersExpectedFields(t *testing.T) {
	opts := &mailpitInstallOptions{
		relayHost:        "smtp.gmail.com",
		relayPort:        587,
		relayUsername:    "lab@example.com",
		relayPassword:    "app:password with spaces",
		relayFrom:        "lab@example.com",
		relayAuth:        "plain",
		relayStartTLS:    true,
		relayAllowedRcpt: ".*@example\\.com$",
	}
	out := opts.mailpitRelayConfigYAML()
	wants := []string{
		"host: smtp.gmail.com",
		"port: 587",
		"starttls: true",
		"auth: plain",
		`username: "lab@example.com"`,
		`password: "app:password with spaces"`,
		`return-path: "lab@example.com"`,
		`allowed-recipients: ".*@example\\.com$"`,
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("relay yaml missing %q\n---\n%s", want, out)
		}
	}
}

func TestMailpitRelayConfigYAMLOmitsBlankFields(t *testing.T) {
	opts := &mailpitInstallOptions{
		relayHost:     "smtp.example.com",
		relayPort:     25,
		relayAuth:     "none",
		relayStartTLS: false,
	}
	out := opts.mailpitRelayConfigYAML()
	for _, mustOmit := range []string{"username:", "password:", "return-path:", "allowed-recipients:"} {
		if strings.Contains(out, mustOmit) {
			t.Fatalf("relay yaml should omit %q for no-auth config:\n%s", mustOmit, out)
		}
	}
	if !strings.Contains(out, "starttls: false") || !strings.Contains(out, "auth: none") {
		t.Fatalf("auth/starttls not propagated:\n%s", out)
	}
}

func TestMailpitManifestsIncludeRelaySecretOnlyWhenEnabled(t *testing.T) {
	opts := &mailpitInstallOptions{
		namespace: "mas-est",
		name:      "mas-mailpit",
		image:     "ghcr.io/axllent/mailpit:v1.30.1",
		routeHost: "mas-mailpit.apps.example.com",
	}
	disabled := opts.manifests()
	for _, m := range disabled {
		if m["kind"] == "Secret" && m["metadata"].(map[string]any)["name"] == mailpitRelaySecretName {
			t.Fatalf("relay Secret should NOT be emitted when relay disabled, got %v", m)
		}
	}

	opts.relayHost = "smtp.example.com"
	opts.relayPort = 587
	opts.relayUsername = "u"
	opts.relayPassword = "p"
	opts.relayAuth = "plain"
	opts.relayStartTLS = true
	enabled := opts.manifests()
	found := false
	for _, m := range enabled {
		if m["kind"] == "Secret" && m["metadata"].(map[string]any)["name"] == mailpitRelaySecretName {
			found = true
			stringData := m["stringData"].(map[string]string)
			if !strings.Contains(stringData[mailpitRelayConfigKey], "host: smtp.example.com") {
				t.Fatalf("relay Secret payload missing host: %q", stringData[mailpitRelayConfigKey])
			}
		}
	}
	if !found {
		t.Fatal("relay Secret missing when relay enabled")
	}
}

func TestMailpitDeploymentMountsRelaySecretOnlyWhenEnabled(t *testing.T) {
	opts := &mailpitInstallOptions{
		namespace: "mas-est",
		name:      "mas-mailpit",
		image:     "ghcr.io/axllent/mailpit:v1.30.1",
	}
	// Disabled: no relay env var, no volume mount, no volume
	manifest := mailpitDeploymentManifest(opts)
	podSpec := manifest["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	if _, present := podSpec["volumes"]; present {
		t.Fatalf("deployment should have no volumes when relay disabled, got %v", podSpec["volumes"])
	}
	containers := podSpec["containers"].([]map[string]any)
	for _, e := range containers[0]["env"].([]map[string]any) {
		if e["name"] == "MP_SMTP_RELAY_CONFIG" {
			t.Fatalf("MP_SMTP_RELAY_CONFIG should be absent when relay disabled, got %v", e)
		}
	}

	// Enabled: env var + mount + volume present
	opts.relayHost = "smtp.example.com"
	opts.relayPort = 587
	opts.relayAuth = "plain"
	opts.relayStartTLS = true
	manifest = mailpitDeploymentManifest(opts)
	podSpec = manifest["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	volumes := podSpec["volumes"].([]map[string]any)
	if len(volumes) != 1 || volumes[0]["name"] != mailpitRelayVolumeName {
		t.Fatalf("expected one relay volume, got %v", volumes)
	}
	secretSrc := volumes[0]["secret"].(map[string]any)
	if secretSrc["secretName"] != mailpitRelaySecretName {
		t.Fatalf("volume not sourced from %s: %v", mailpitRelaySecretName, secretSrc)
	}
	containers = podSpec["containers"].([]map[string]any)
	foundEnv := false
	for _, e := range containers[0]["env"].([]map[string]any) {
		if e["name"] == "MP_SMTP_RELAY_CONFIG" && e["value"] == mailpitRelayConfigPath {
			foundEnv = true
		}
	}
	if !foundEnv {
		t.Fatal("MP_SMTP_RELAY_CONFIG env var missing on relay-enabled deployment")
	}
	mounts := containers[0]["volumeMounts"].([]map[string]any)
	if len(mounts) != 1 || mounts[0]["mountPath"] != mailpitRelayConfigPath || mounts[0]["subPath"] != mailpitRelayConfigKey {
		t.Fatalf("relay volume mount missing or wrong: %v", mounts)
	}
}

func TestMailpitRouteManifestTargetsUI(t *testing.T) {
	opts := &mailpitInstallOptions{
		namespace: "mas-est",
		name:      "mas-mailpit",
		routeHost: "mas-mailpit.apps.example.com",
	}

	manifest := mailpitRouteManifest(opts)
	spec := manifest["spec"].(map[string]any)
	if spec["host"] != "mas-mailpit.apps.example.com" {
		t.Fatalf("host = %q", spec["host"])
	}
	port := spec["port"].(map[string]string)
	if port["targetPort"] != "ui" {
		t.Fatalf("targetPort = %q", port["targetPort"])
	}
}
