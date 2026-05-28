package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	executil "github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/exec"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/ui"
)

const (
	defaultRookNamespace       = "rook-ceph"
	defaultObjectStoreName     = "mas-s3"
	defaultObjectStorageClass  = "rook-ceph-bucket"
	defaultObjectBucketClaim   = "mas-s3-bucket"
	defaultObjectStorageRegion = "us-east-1"
)

type objectStorageInstallOptions struct {
	masInstanceID       string
	masCoreNamespace    string
	rookNamespace       string
	storeName           string
	storageClassName    string
	bucketClaimName     string
	bucketGenerateName  string
	routeHost           string
	certIssuerName      string
	certIssuerKind      string
	certSecretName      string
	masCredentialsName  string
	objectStorageCfg    string
	displayName         string
	replicationSize     int
	timeout             time.Duration
	skipMASConfig       bool
	skipCertificate     bool
	skipRoute           bool
	preservePoolsOnDrop bool
}

type s3BucketDetails struct {
	AccessKey string
	SecretKey string
	Bucket    string
	Host      string
	Port      string
	Region    string
}

func newObjectStorageCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "object-storage",
		Short: "Install and inspect MAS object storage helpers",
	}
	command.AddCommand(newObjectStorageInstallRookCephCommand())
	return command
}

func newObjectStorageInstallRookCephCommand() *cobra.Command {
	opts := &objectStorageInstallOptions{
		rookNamespace:       defaultRookNamespace,
		storeName:           defaultObjectStoreName,
		storageClassName:    defaultObjectStorageClass,
		bucketClaimName:     defaultObjectBucketClaim,
		bucketGenerateName:  defaultObjectBucketClaim,
		certIssuerKind:      "ClusterIssuer",
		replicationSize:     3,
		timeout:             10 * time.Minute,
		preservePoolsOnDrop: true,
	}

	command := &cobra.Command{
		Use:   "install-rook-ceph",
		Short: "Create a Rook Ceph RGW S3 endpoint and MAS ObjectStorageCfg",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.run(cmd.Context())
		},
	}

	flags := command.Flags()
	flags.StringVar(&opts.masInstanceID, "mas-instance-id", opts.masInstanceID, "MAS instance ID, for example lfmas")
	flags.StringVar(&opts.masCoreNamespace, "mas-core-namespace", opts.masCoreNamespace, "MAS core namespace, for example mas-lfmas-core")
	flags.StringVar(&opts.rookNamespace, "rook-namespace", opts.rookNamespace, "Namespace containing the Rook Ceph cluster")
	flags.StringVar(&opts.storeName, "store-name", opts.storeName, "CephObjectStore name to create")
	flags.StringVar(&opts.storageClassName, "bucket-storage-class", opts.storageClassName, "ObjectBucketClaim storage class name to create/use")
	flags.StringVar(&opts.bucketClaimName, "bucket-claim", opts.bucketClaimName, "ObjectBucketClaim name to create in the MAS core namespace")
	flags.StringVar(&opts.bucketGenerateName, "bucket-generate-name", opts.bucketGenerateName, "Generated bucket name prefix")
	flags.StringVar(&opts.routeHost, "route-host", opts.routeHost, "External S3 route host, for example mas-s3.apps.example.com")
	flags.StringVar(&opts.certIssuerName, "cert-issuer-name", opts.certIssuerName, "cert-manager issuer name for the RGW route certificate")
	flags.StringVar(&opts.certIssuerKind, "cert-issuer-kind", opts.certIssuerKind, "cert-manager issuer kind: ClusterIssuer or Issuer")
	flags.StringVar(&opts.certSecretName, "cert-secret", opts.certSecretName, "TLS secret name used by Rook RGW")
	flags.StringVar(&opts.masCredentialsName, "mas-credentials-secret", opts.masCredentialsName, "MAS ObjectStorageCfg credential secret to create")
	flags.StringVar(&opts.objectStorageCfg, "objectstoragecfg-name", opts.objectStorageCfg, "MAS ObjectStorageCfg name to create")
	flags.StringVar(&opts.displayName, "display-name", opts.displayName, "MAS ObjectStorageCfg display name")
	flags.IntVar(&opts.replicationSize, "replication-size", opts.replicationSize, "Ceph object store pool replication size")
	flags.DurationVar(&opts.timeout, "timeout", opts.timeout, "Time to wait for generated bucket credentials")
	flags.BoolVar(&opts.skipMASConfig, "skip-mas-config", opts.skipMASConfig, "Only create the Rook/Ceph S3 endpoint and bucket")
	flags.BoolVar(&opts.skipCertificate, "skip-certificate", opts.skipCertificate, "Do not create a cert-manager certificate for RGW")
	flags.BoolVar(&opts.skipRoute, "skip-route", opts.skipRoute, "Do not create an OpenShift Route for RGW")
	flags.BoolVar(&opts.preservePoolsOnDrop, "preserve-pools-on-delete", opts.preservePoolsOnDrop, "Preserve Ceph pools if the CephObjectStore is deleted")

	return command
}

func (o *objectStorageInstallOptions) run(ctx context.Context) error {
	runner := executil.NewRunner()
	client := oc.NewClient(runner)
	if _, err := ensureClusterLogin(ctx, runner, client, ui.IsInteractive()); err != nil {
		return err
	}
	if err := o.defaultAndValidate(ctx); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "[object-storage] installing Rook Ceph S3 endpoint %s in namespace %s\n", o.storeName, o.rookNamespace)

	for _, manifest := range o.installManifests() {
		if err := applyObjectStorageManifest(ctx, client, manifest); err != nil {
			return err
		}
	}

	rgwDeployment := "deployment/rook-ceph-rgw-" + o.storeName + "-a"
	if err := waitForDeployment(ctx, client, o.rookNamespace, "rook-ceph-rgw-"+o.storeName+"-a", o.timeout); err != nil {
		return err
	}
	if _, err := client.RolloutStatus(ctx, o.rookNamespace, rgwDeployment, o.timeout.String()); err != nil {
		return fmt.Errorf("wait for %s in namespace %s: %w", rgwDeployment, o.rookNamespace, err)
	}
	fmt.Fprintf(os.Stdout, "[object-storage] RGW rollout complete: %s/%s\n", o.rookNamespace, rgwDeployment)

	details, err := o.waitForBucketDetails(ctx, client)
	if err != nil {
		return err
	}

	endpointURL := o.endpointURL(details)
	fmt.Fprintf(os.Stdout, "[object-storage] bucket ready: %s\n", details.Bucket)
	fmt.Fprintf(os.Stdout, "[object-storage] endpoint: %s\n", endpointURL)

	if o.skipMASConfig {
		o.printSummary(details, endpointURL)
		return nil
	}

	caCert, err := o.waitForCertificateForMAS(ctx, client)
	if err != nil {
		return err
	}
	if err := o.applyMASConfig(ctx, client, details, endpointURL, caCert); err != nil {
		return err
	}
	if err := waitForObjectStorageCfgReady(ctx, client, o.masCoreNamespace, o.objectStorageCfg, o.timeout); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "[object-storage] MAS ObjectStorageCfg ready: %s/%s\n", o.masCoreNamespace, o.objectStorageCfg)
	o.printSummary(details, endpointURL)
	return nil
}

func (o *objectStorageInstallOptions) defaultAndValidate(ctx context.Context) error {
	o.masInstanceID = strings.TrimSpace(o.masInstanceID)
	o.masCoreNamespace = strings.TrimSpace(o.masCoreNamespace)
	if o.masCoreNamespace == "" && o.masInstanceID != "" {
		o.masCoreNamespace = "mas-" + o.masInstanceID + "-core"
	}
	if o.masInstanceID == "" && strings.HasPrefix(o.masCoreNamespace, "mas-") && strings.HasSuffix(o.masCoreNamespace, "-core") {
		o.masInstanceID = strings.TrimSuffix(strings.TrimPrefix(o.masCoreNamespace, "mas-"), "-core")
	}
	if o.masInstanceID == "" {
		return fmt.Errorf("--mas-instance-id is required")
	}
	if o.masCoreNamespace == "" {
		return fmt.Errorf("--mas-core-namespace is required")
	}

	o.rookNamespace = defaultString(o.rookNamespace, defaultRookNamespace)
	o.storeName = defaultString(o.storeName, defaultObjectStoreName)
	o.storageClassName = defaultString(o.storageClassName, defaultObjectStorageClass)
	o.bucketClaimName = defaultString(o.bucketClaimName, defaultObjectBucketClaim)
	o.bucketGenerateName = defaultString(o.bucketGenerateName, o.bucketClaimName)
	o.certSecretName = defaultString(o.certSecretName, o.storeName+"-rgw-tls")
	o.masCredentialsName = defaultString(o.masCredentialsName, o.storeName+"-objectstorage-credentials")
	o.objectStorageCfg = defaultString(o.objectStorageCfg, fmt.Sprintf("ibm-mas-%s-objectstoragecfg-system", o.masInstanceID))
	o.displayName = defaultString(o.displayName, "MAS S3 Demo Object Storage")
	o.certIssuerKind = defaultString(o.certIssuerKind, "ClusterIssuer")
	if o.replicationSize == 0 {
		o.replicationSize = 3
	}
	if o.replicationSize < 1 {
		return fmt.Errorf("--replication-size must be at least 1")
	}
	if o.routeHost == "" && !o.skipRoute {
		host, err := defaultObjectStorageRouteHost(ctx, o.storeName)
		if err != nil {
			return err
		}
		o.routeHost = host
	}
	if !o.skipCertificate && o.certIssuerName == "" {
		o.certIssuerName = "mas-" + o.masInstanceID + "-ca"
	}
	return nil
}

func (o *objectStorageInstallOptions) installManifests() []map[string]any {
	manifests := []map[string]any{}
	if !o.skipCertificate {
		manifests = append(manifests, certificateManifest(o))
	}
	manifests = append(manifests, cephObjectStoreManifest(o))
	if !o.skipRoute {
		manifests = append(manifests, routeManifest(o))
	}
	manifests = append(manifests, bucketStorageClassManifest(o), objectBucketClaimManifest(o))
	return manifests
}

func (o *objectStorageInstallOptions) waitForBucketDetails(ctx context.Context, client *oc.Client) (s3BucketDetails, error) {
	deadline := time.Now().Add(o.timeout)
	var lastErr error
	for {
		details, err := o.bucketDetails(ctx, client)
		if err == nil {
			return details, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return s3BucketDetails{}, fmt.Errorf("wait for ObjectBucketClaim %s/%s credentials: %w", o.masCoreNamespace, o.bucketClaimName, lastErr)
		}
		time.Sleep(5 * time.Second)
	}
}

func (o *objectStorageInstallOptions) bucketDetails(ctx context.Context, client *oc.Client) (s3BucketDetails, error) {
	secret, err := client.SecretData(ctx, o.masCoreNamespace, o.bucketClaimName)
	if err != nil {
		return s3BucketDetails{}, err
	}
	configMap, err := client.ConfigMapData(ctx, o.masCoreNamespace, o.bucketClaimName)
	if err != nil {
		return s3BucketDetails{}, err
	}
	accessKey, secretKey, err := objectBucketCredentials(secret)
	if err != nil {
		return s3BucketDetails{}, err
	}
	bucket := firstNonEmpty(configMap["BUCKET_NAME"], configMap["bucketName"])
	if bucket == "" {
		return s3BucketDetails{}, fmt.Errorf("configmap/%s does not contain BUCKET_NAME", o.bucketClaimName)
	}
	return s3BucketDetails{
		AccessKey: accessKey,
		SecretKey: secretKey,
		Bucket:    bucket,
		Host:      firstNonEmpty(configMap["BUCKET_HOST"], configMap["host"]),
		Port:      firstNonEmpty(configMap["BUCKET_PORT"], configMap["port"]),
		Region:    firstNonEmpty(configMap["BUCKET_REGION"], configMap["region"], defaultObjectStorageRegion),
	}, nil
}

func (o *objectStorageInstallOptions) endpointURL(details s3BucketDetails) string {
	if o.routeHost != "" {
		return "https://" + o.routeHost
	}
	if details.Port != "" {
		return fmt.Sprintf("http://%s:%s", details.Host, details.Port)
	}
	return "http://" + details.Host
}

func (o *objectStorageInstallOptions) waitForCertificateForMAS(ctx context.Context, client *oc.Client) (string, error) {
	if o.skipCertificate {
		return "", nil
	}
	deadline := time.Now().Add(o.timeout)
	var lastErr error
	for {
		secret, err := client.SecretData(ctx, o.rookNamespace, o.certSecretName)
		if err == nil {
			cert := firstNonEmpty(secret["ca.crt"], secret["tls.crt"])
			if cert != "" {
				return cert, nil
			}
			err = fmt.Errorf("secret/%s does not contain ca.crt or tls.crt", o.certSecretName)
		}
		lastErr = err
		if time.Now().After(deadline) {
			return "", fmt.Errorf("wait for RGW certificate secret %s/%s: %w", o.rookNamespace, o.certSecretName, lastErr)
		}
		time.Sleep(5 * time.Second)
	}
}

func (o *objectStorageInstallOptions) applyMASConfig(ctx context.Context, client *oc.Client, details s3BucketDetails, endpointURL, caCert string) error {
	credentialSecret := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"type":       "Opaque",
		"metadata": map[string]any{
			"name":      o.masCredentialsName,
			"namespace": o.masCoreNamespace,
			"labels": map[string]string{
				"app.kubernetes.io/instance":   o.masInstanceID,
				"app.kubernetes.io/managed-by": "mas-iam-installer",
				"app.kubernetes.io/name":       "mas-external-services",
				"mas.ibm.com/instanceId":       o.masInstanceID,
			},
		},
		"stringData": map[string]string{
			"username": details.AccessKey,
			"password": details.SecretKey,
		},
	}
	if err := applyObjectStorageManifest(ctx, client, credentialSecret); err != nil {
		return err
	}

	objectStorageCfg := map[string]any{
		"apiVersion": "config.mas.ibm.com/v1",
		"kind":       "ObjectStorageCfg",
		"metadata": map[string]any{
			"name":      o.objectStorageCfg,
			"namespace": o.masCoreNamespace,
			"labels": map[string]string{
				"app.kubernetes.io/instance":   "ibm-mas",
				"app.kubernetes.io/managed-by": "mas-iam-installer",
				"app.kubernetes.io/name":       "ibm-mas",
				"mas.ibm.com/configScope":      "system",
				"mas.ibm.com/instanceId":       o.masInstanceID,
			},
		},
		"spec": map[string]any{
			"displayName": o.displayName,
			"config": map[string]any{
				"url": endpointURL,
				"credentials": map[string]string{
					"secretName": o.masCredentialsName,
				},
			},
		},
	}
	if caCert != "" {
		objectStorageCfg["spec"].(map[string]any)["certificates"] = []map[string]string{{
			"alias": o.storeName + "-ca",
			"crt":   caCert,
		}}
	}
	return applyObjectStorageManifest(ctx, client, objectStorageCfg)
}

func (o *objectStorageInstallOptions) printSummary(details s3BucketDetails, endpointURL string) {
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "S3 details")
	fmt.Fprintf(os.Stdout, "  Endpoint URL: %s\n", endpointURL)
	fmt.Fprintf(os.Stdout, "  Bucket: %s\n", details.Bucket)
	fmt.Fprintf(os.Stdout, "  Region: %s\n", details.Region)
	fmt.Fprintf(os.Stdout, "  Access key secret: %s/%s key AWS_ACCESS_KEY_ID\n", o.masCoreNamespace, o.bucketClaimName)
	fmt.Fprintf(os.Stdout, "  Secret key secret: %s/%s key AWS_SECRET_ACCESS_KEY\n", o.masCoreNamespace, o.bucketClaimName)
	if !o.skipMASConfig {
		fmt.Fprintf(os.Stdout, "  MAS credential secret: %s/%s keys username,password\n", o.masCoreNamespace, o.masCredentialsName)
		fmt.Fprintf(os.Stdout, "  MAS ObjectStorageCfg: %s/%s\n", o.masCoreNamespace, o.objectStorageCfg)
	}
}

func applyObjectStorageManifest(ctx context.Context, client *oc.Client, manifest map[string]any) error {
	raw, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	if _, err := client.Apply(ctx, raw); err != nil {
		return err
	}
	return nil
}

func waitForDeployment(ctx context.Context, client *oc.Client, namespace, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		if _, err := client.Deployment(ctx, namespace, name); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wait for deployment/%s in namespace %s: %w", name, namespace, lastErr)
		}
		time.Sleep(5 * time.Second)
	}
}

func waitForObjectStorageCfgReady(ctx context.Context, client *oc.Client, namespace, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastState string
	for {
		conditions, err := client.ObjectStorageCfgConditions(ctx, namespace, name)
		if err == nil {
			ready, state := objectStorageReadyState(conditions)
			if ready {
				return nil
			}
			lastState = state
		} else {
			lastState = err.Error()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wait for objectstoragecfg/%s in namespace %s to become Ready: %s", name, namespace, lastState)
		}
		time.Sleep(5 * time.Second)
	}
}

func objectStorageReadyState(conditions []oc.ConditionStatus) (bool, string) {
	if len(conditions) == 0 {
		return false, "no status conditions yet"
	}
	summaries := make([]string, 0, len(conditions))
	for _, condition := range conditions {
		summary := fmt.Sprintf("%s=%s", condition.Type, condition.Status)
		if condition.Reason != "" {
			summary += " " + condition.Reason
		}
		if condition.Message != "" {
			summary += " - " + condition.Message
		}
		summaries = append(summaries, summary)
		if condition.Type == "Ready" && condition.Status == "True" {
			return true, strings.Join(summaries, "; ")
		}
	}
	return false, strings.Join(summaries, "; ")
}

func certificateManifest(o *objectStorageInstallOptions) map[string]any {
	dnsNames := []string{
		"rook-ceph-rgw-" + o.storeName + "." + o.rookNamespace + ".svc",
		"rook-ceph-rgw-" + o.storeName + "." + o.rookNamespace + ".svc.cluster.local",
	}
	if o.routeHost != "" {
		dnsNames = append([]string{o.routeHost}, dnsNames...)
	}
	return map[string]any{
		"apiVersion": "cert-manager.io/v1",
		"kind":       "Certificate",
		"metadata": map[string]any{
			"name":      o.certSecretName,
			"namespace": o.rookNamespace,
		},
		"spec": map[string]any{
			"secretName": o.certSecretName,
			"issuerRef": map[string]string{
				"name": o.certIssuerName,
				"kind": o.certIssuerKind,
			},
			"dnsNames": dnsNames,
		},
	}
}

func cephObjectStoreManifest(o *objectStorageInstallOptions) map[string]any {
	gateway := map[string]any{
		"instances":  1,
		"port":       80,
		"securePort": 443,
	}
	if !o.skipCertificate {
		gateway["sslCertificateRef"] = o.certSecretName
	}
	return map[string]any{
		"apiVersion": "ceph.rook.io/v1",
		"kind":       "CephObjectStore",
		"metadata": map[string]any{
			"name":      o.storeName,
			"namespace": o.rookNamespace,
		},
		"spec": map[string]any{
			"metadataPool": map[string]any{
				"failureDomain": "host",
				"replicated": map[string]int{
					"size": o.replicationSize,
				},
			},
			"dataPool": map[string]any{
				"failureDomain": "host",
				"replicated": map[string]int{
					"size": o.replicationSize,
				},
			},
			"preservePoolsOnDelete": o.preservePoolsOnDrop,
			"gateway":               gateway,
		},
	}
}

func routeManifest(o *objectStorageInstallOptions) map[string]any {
	return map[string]any{
		"apiVersion": "route.openshift.io/v1",
		"kind":       "Route",
		"metadata": map[string]any{
			"name":      o.storeName,
			"namespace": o.rookNamespace,
		},
		"spec": map[string]any{
			"host": o.routeHost,
			"to": map[string]string{
				"kind": "Service",
				"name": "rook-ceph-rgw-" + o.storeName,
			},
			"port": map[string]string{
				"targetPort": "https",
			},
			"tls": map[string]string{
				"termination":                   "passthrough",
				"insecureEdgeTerminationPolicy": "Redirect",
			},
		},
	}
}

func bucketStorageClassManifest(o *objectStorageInstallOptions) map[string]any {
	return map[string]any{
		"apiVersion": "storage.k8s.io/v1",
		"kind":       "StorageClass",
		"metadata": map[string]string{
			"name": o.storageClassName,
		},
		"provisioner": "rook-ceph.ceph.rook.io/bucket",
		"parameters": map[string]string{
			"objectStoreName":      o.storeName,
			"objectStoreNamespace": o.rookNamespace,
			"region":               defaultObjectStorageRegion,
		},
		"reclaimPolicy": "Delete",
	}
}

func objectBucketClaimManifest(o *objectStorageInstallOptions) map[string]any {
	return map[string]any{
		"apiVersion": "objectbucket.io/v1alpha1",
		"kind":       "ObjectBucketClaim",
		"metadata": map[string]any{
			"name":      o.bucketClaimName,
			"namespace": o.masCoreNamespace,
		},
		"spec": map[string]string{
			"generateBucketName": o.bucketGenerateName,
			"storageClassName":   o.storageClassName,
		},
	}
}

func objectBucketCredentials(secret map[string]string) (string, string, error) {
	accessKey := firstNonEmpty(secret["AWS_ACCESS_KEY_ID"], secret["AccessKey"], secret["accessKey"], secret["access_key"])
	secretKey := firstNonEmpty(secret["AWS_SECRET_ACCESS_KEY"], secret["SecretKey"], secret["secretKey"], secret["secret_key"])
	if accessKey == "" || secretKey == "" {
		return "", "", fmt.Errorf("bucket credential secret does not contain recognizable access and secret key fields")
	}
	return accessKey, secretKey, nil
}

func defaultObjectStorageRouteHost(ctx context.Context, storeName string) (string, error) {
	runner := executil.NewRunner()
	output, err := runner.Output(ctx, executil.Options{
		Name: "oc",
		Args: []string{"get", "ingresscontroller", "default", "-n", "openshift-ingress-operator", "-o", "jsonpath={.status.domain}"},
	})
	if err != nil {
		return "", fmt.Errorf("determine OpenShift route domain; provide --route-host to skip autodetection: %w", err)
	}
	domain := strings.TrimSpace(output)
	if domain == "" {
		return "", fmt.Errorf("determine OpenShift route domain; provide --route-host")
	}
	return storeName + "." + domain, nil
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
