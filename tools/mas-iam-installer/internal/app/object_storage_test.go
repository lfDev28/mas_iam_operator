package app

import (
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
		namespace: "mas-external-services",
		name:      "mas-minio",
	}

	if got := opts.internalEndpointURL(); got != "http://mas-minio.mas-external-services.svc.cluster.local:9000" {
		t.Fatalf("internalEndpointURL() = %q", got)
	}
}

func TestMinIORoutesUseSeparateServicePorts(t *testing.T) {
	opts := &minioInstallOptions{
		namespace: "mas-external-services",
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
