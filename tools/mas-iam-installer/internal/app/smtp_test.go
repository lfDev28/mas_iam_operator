package app

import "testing"

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
	}
	for k, want := range wants {
		if data[k] != want {
			t.Fatalf("data[%q] = %q, want %q", k, data[k], want)
		}
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
