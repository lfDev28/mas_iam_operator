package app

import (
	"strings"
	"testing"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
)

func TestObjectBucketCredentialsKnownKeys(t *testing.T) {
	tests := []struct {
		name   string
		secret map[string]string
	}{
		{
			name: "aws style",
			secret: map[string]string{
				"AWS_ACCESS_KEY_ID":     "access",
				"AWS_SECRET_ACCESS_KEY": "secret",
			},
		},
		{
			name: "rook legacy style",
			secret: map[string]string{
				"AccessKey": "access",
				"SecretKey": "secret",
			},
		},
		{
			name: "lower camel style",
			secret: map[string]string{
				"accessKey": "access",
				"secretKey": "secret",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			accessKey, secretKey, err := objectBucketCredentials(test.secret)
			if err != nil {
				t.Fatalf("objectBucketCredentials() error = %v", err)
			}
			if accessKey != "access" || secretKey != "secret" {
				t.Fatalf("objectBucketCredentials() = %q, %q", accessKey, secretKey)
			}
		})
	}
}

func TestObjectBucketCredentialsMissingKeys(t *testing.T) {
	if _, _, err := objectBucketCredentials(map[string]string{"username": "access"}); err == nil {
		t.Fatal("objectBucketCredentials() expected error")
	}
}

func TestObjectStorageDefaultAndValidateDerivesInstanceID(t *testing.T) {
	opts := &objectStorageInstallOptions{
		masCoreNamespace: "mas-lfmas-core",
		skipRoute:        true,
		skipCertificate:  true,
	}

	if err := opts.defaultAndValidate(t.Context()); err != nil {
		t.Fatalf("defaultAndValidate() error = %v", err)
	}

	if opts.masInstanceID != "lfmas" {
		t.Fatalf("masInstanceID = %q, want lfmas", opts.masInstanceID)
	}
	if opts.objectStorageCfg != "ibm-mas-lfmas-objectstoragecfg-system" {
		t.Fatalf("objectStorageCfg = %q", opts.objectStorageCfg)
	}
	if opts.masCredentialsName != "mas-s3-objectstorage-credentials" {
		t.Fatalf("masCredentialsName = %q", opts.masCredentialsName)
	}
}

func TestMinIOInternalEndpointURL(t *testing.T) {
	opts := &minioInstallOptions{
		namespace: "mas-est",
		name:      "mas-minio",
	}

	if got := opts.internalEndpointURL(); got != "http://mas-minio.mas-est.svc.cluster.local:9000" {
		t.Fatalf("internalEndpointURL() = %q", got)
	}
	if got := opts.adminEndpointURL(); got != "http://mas-minio:9000" {
		t.Fatalf("adminEndpointURL() = %q", got)
	}
}

func TestMinIOManageEndpointURL(t *testing.T) {
	opts := &minioInstallOptions{
		namespace: "mas-est",
	}

	if got := opts.manageEndpointURL(); got != "http://mas-est.svc.cluster.local:9000" {
		t.Fatalf("manageEndpointURL() = %q", got)
	}
	if got := opts.virtualHostDomain(); got != "mas-est.svc.cluster.local" {
		t.Fatalf("virtualHostDomain() = %q", got)
	}
}

func TestMinIOManageBucketNames(t *testing.T) {
	opts := &minioInstallOptions{bucket: "mas-s3-demo"}

	got := opts.manageBucketNames()
	want := []string{"mas-s3-demo", "mas-s3-demorecovery", "mas-s3-demobackup"}
	if len(got) != len(want) {
		t.Fatalf("manageBucketNames() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("manageBucketNames() = %v, want %v", got, want)
		}
	}
}

func TestMinIOBucketAliasServiceManifest(t *testing.T) {
	opts := &minioInstallOptions{
		namespace: "mas-est",
		name:      "mas-minio",
	}

	manifest := minioBucketAliasServiceManifest(opts, "mas-s3-demorecovery")
	metadata := manifest["metadata"].(map[string]any)
	if metadata["name"] != "mas-s3-demorecovery" {
		t.Fatalf("service name = %q", metadata["name"])
	}
	spec := manifest["spec"].(map[string]any)
	selector := spec["selector"].(map[string]string)
	if selector["app.kubernetes.io/instance"] != "mas-minio" {
		t.Fatalf("selector = %v", selector)
	}
	ports := spec["ports"].([]map[string]any)
	if ports[0]["port"] != 9000 || ports[0]["targetPort"] != 9000 {
		t.Fatalf("ports = %v", ports)
	}
}

func TestMinIODeploymentCanEnableVirtualHostDomain(t *testing.T) {
	opts := &minioInstallOptions{
		namespace:      "mas-est",
		name:           "mas-minio",
		image:          "quay.io/minio/minio:latest",
		rootSecretName: "mas-minio-root",
	}

	manifest := minioDeploymentManifest(opts, true)
	spec := manifest["spec"].(map[string]any)
	template := spec["template"].(map[string]any)
	podSpec := template["spec"].(map[string]any)
	containers := podSpec["containers"].([]map[string]any)
	env := containers[0]["env"].([]map[string]any)

	found := false
	for _, item := range env {
		if item["name"] == "MINIO_DOMAIN" {
			found = true
			if item["value"] != "mas-est.svc.cluster.local" {
				t.Fatalf("MINIO_DOMAIN = %q", item["value"])
			}
		}
	}
	if !found {
		t.Fatal("MINIO_DOMAIN env var was not rendered")
	}
}

func TestMinIOBucketInitJobCreatesManageLayout(t *testing.T) {
	opts := &minioInstallOptions{
		namespace:      "mas-est",
		name:           "mas-minio",
		bucket:         "mas-s3-demo",
		mcImage:        "quay.io/minio/mc:latest",
		rootSecretName: "mas-minio-root",
	}

	manifest := minioBucketInitJobManifest(opts, "mas-minio-bucket-init")
	spec := manifest["spec"].(map[string]any)
	template := spec["template"].(map[string]any)
	podSpec := template["spec"].(map[string]any)
	containers := podSpec["containers"].([]map[string]any)
	args := containers[0]["args"].([]string)
	script := args[0]
	env := containers[0]["env"].([]map[string]any)

	for _, expected := range []string{
		"http://mas-minio:9000",
		"MINIO_MANAGE_BUCKETS",
		"MINIO_SERVICE_BUCKET",
		"local/$MINIO_SERVICE_BUCKET",
		"local/$MINIO_BUCKET/recovery/.keep",
		"local/$MINIO_BUCKET/backup/.keep",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("bucket init script missing %q:\n%s", expected, script)
		}
	}
	foundBuckets := false
	foundServiceBucket := false
	for _, item := range env {
		if item["name"] == "MINIO_MANAGE_BUCKETS" {
			foundBuckets = true
			if item["value"] != "mas-s3-demo mas-s3-demorecovery mas-s3-demobackup" {
				t.Fatalf("MINIO_MANAGE_BUCKETS = %q", item["value"])
			}
		}
		if item["name"] == "MINIO_SERVICE_BUCKET" {
			foundServiceBucket = true
			if item["value"] != "mas-minio" {
				t.Fatalf("MINIO_SERVICE_BUCKET = %q", item["value"])
			}
		}
	}
	if !foundBuckets {
		t.Fatal("MINIO_MANAGE_BUCKETS env var was not rendered")
	}
	if !foundServiceBucket {
		t.Fatal("MINIO_SERVICE_BUCKET env var was not rendered")
	}
}

func TestMinIORoutesUseSeparateServicePorts(t *testing.T) {
	opts := &minioInstallOptions{
		namespace: "mas-est",
		name:      "mas-minio",
	}

	apiRoute := minioRouteManifest(opts, "mas-minio-api", "minio-api.example.com", "api")
	apiSpec := apiRoute["spec"].(map[string]any)
	apiPort := apiSpec["port"].(map[string]string)
	if apiPort["targetPort"] != "api" {
		t.Fatalf("api targetPort = %q", apiPort["targetPort"])
	}

	consoleRoute := minioRouteManifest(opts, "mas-minio-console", "minio-console.example.com", "console")
	consoleSpec := consoleRoute["spec"].(map[string]any)
	consolePort := consoleSpec["port"].(map[string]string)
	if consolePort["targetPort"] != "console" {
		t.Fatalf("console targetPort = %q", consolePort["targetPort"])
	}
}

func TestCertificateManifestSkipsEmptyRouteHost(t *testing.T) {
	opts := &objectStorageInstallOptions{
		rookNamespace:  "rook-ceph",
		storeName:      "mas-s3",
		certSecretName: "mas-s3-rgw-tls",
		certIssuerName: "mas-lfmas-ca",
		certIssuerKind: "ClusterIssuer",
	}

	manifest := certificateManifest(opts)
	spec := manifest["spec"].(map[string]any)
	dnsNames := spec["dnsNames"].([]string)
	for _, name := range dnsNames {
		if name == "" {
			t.Fatal("certificateManifest() included empty DNS name")
		}
	}
}

func TestObjectStorageReadyState(t *testing.T) {
	ready, state := objectStorageReadyState([]oc.ConditionStatus{
		{Type: "Running", Status: "True", Reason: "Running", Message: "Running reconciliation"},
		{Type: "Ready", Status: "True", Reason: "Ready", Message: "Ready"},
	})
	if !ready {
		t.Fatalf("ready = false, state = %s", state)
	}
}

func TestObjectStorageReadyStateNotReady(t *testing.T) {
	ready, state := objectStorageReadyState([]oc.ConditionStatus{
		{Type: "Ready", Status: "False", Reason: "ValidationFailed", Message: "bad endpoint"},
	})
	if ready {
		t.Fatal("ready = true, want false")
	}
	if state != "Ready=False ValidationFailed - bad endpoint" {
		t.Fatalf("state = %q", state)
	}
}
