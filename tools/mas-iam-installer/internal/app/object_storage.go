package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
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
	defaultMinIONamespace      = config.DefaultNamespace
	defaultMinIOName           = "mas-minio"
	defaultMinIOBucket         = "mas-s3-demo"
	defaultMinIOPVCSize        = "20Gi"
	defaultMinIOStorageClass   = "rook-ceph-block"
	defaultMinIOImage          = "quay.io/minio/minio:latest"
	defaultMinIOMCImage        = "quay.io/minio/mc:latest"
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

type minioInstallOptions struct {
	masInstanceID      string
	masCoreNamespace   string
	namespace          string
	name               string
	bucket             string
	storageClassName   string
	pvcSize            string
	image              string
	mcImage            string
	rootSecretName     string
	masCredentialsName string
	objectStorageCfg   string
	displayName        string
	apiRouteHost       string
	consoleRouteHost   string
	timeout            time.Duration
	skipMASConfig      bool
}

func newObjectStorageCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "object-storage",
		Short: "Install and inspect MAS object storage helpers",
	}
	command.AddCommand(
		newObjectStorageInstallRookCephCommand(),
		newObjectStorageInstallMinIOCommand(),
	)
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

func newObjectStorageInstallMinIOCommand() *cobra.Command {
	opts := &minioInstallOptions{
		namespace:        defaultMinIONamespace,
		name:             defaultMinIOName,
		bucket:           defaultMinIOBucket,
		storageClassName: defaultMinIOStorageClass,
		pvcSize:          defaultMinIOPVCSize,
		image:            defaultMinIOImage,
		mcImage:          defaultMinIOMCImage,
		timeout:          10 * time.Minute,
	}

	command := &cobra.Command{
		Use:   "install-minio",
		Short: "Create a MinIO S3 endpoint and MAS ObjectStorageCfg",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.run(cmd.Context())
		},
	}

	flags := command.Flags()
	flags.StringVar(&opts.masInstanceID, "mas-instance-id", opts.masInstanceID, "MAS instance ID, for example lfmas")
	flags.StringVar(&opts.masCoreNamespace, "mas-core-namespace", opts.masCoreNamespace, "MAS core namespace, for example mas-lfmas-core")
	flags.StringVar(&opts.namespace, "namespace", opts.namespace, "Namespace to install MinIO into")
	flags.StringVar(&opts.name, "name", opts.name, "MinIO resource name prefix")
	flags.StringVar(&opts.bucket, "bucket", opts.bucket, "Bucket to create for MAS testing")
	flags.StringVar(&opts.storageClassName, "storage-class", opts.storageClassName, "PVC storage class for MinIO data")
	flags.StringVar(&opts.pvcSize, "pvc-size", opts.pvcSize, "PVC size for MinIO data")
	flags.StringVar(&opts.image, "image", opts.image, "MinIO server image")
	flags.StringVar(&opts.mcImage, "mc-image", opts.mcImage, "MinIO client image used by the bucket init job")
	flags.StringVar(&opts.rootSecretName, "root-secret", opts.rootSecretName, "Secret containing MINIO_ROOT_USER and MINIO_ROOT_PASSWORD")
	flags.StringVar(&opts.masCredentialsName, "mas-credentials-secret", opts.masCredentialsName, "MAS ObjectStorageCfg credential secret to create")
	flags.StringVar(&opts.objectStorageCfg, "objectstoragecfg-name", opts.objectStorageCfg, "MAS ObjectStorageCfg name to create")
	flags.StringVar(&opts.displayName, "display-name", opts.displayName, "MAS ObjectStorageCfg display name")
	flags.StringVar(&opts.apiRouteHost, "api-route-host", opts.apiRouteHost, "External MinIO S3 API route host")
	flags.StringVar(&opts.consoleRouteHost, "console-route-host", opts.consoleRouteHost, "External MinIO Console route host")
	flags.DurationVar(&opts.timeout, "timeout", opts.timeout, "Time to wait for MinIO and MAS ObjectStorageCfg readiness")
	flags.BoolVar(&opts.skipMASConfig, "skip-mas-config", opts.skipMASConfig, "Only create MinIO and the demo bucket")

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
		if err := applyConnectionDetails(ctx, client, o.masCoreNamespace, o.rookDetails(details, endpointURL)); err != nil {
			return err
		}
		if err := applyProviderConnectionSecret(ctx, client, o.masCoreNamespace, s3ConnectionSecretName, "s3", o.s3ConnectionSecretData(details, endpointURL)); err != nil {
			return err
		}
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
	if err := applyConnectionDetails(ctx, client, o.masCoreNamespace, o.rookDetails(details, endpointURL)); err != nil {
		return err
	}
	if err := applyProviderConnectionSecret(ctx, client, o.masCoreNamespace, s3ConnectionSecretName, "s3", o.s3ConnectionSecretData(details, endpointURL)); err != nil {
		return err
	}
	o.printSummary(details, endpointURL)
	return nil
}

func (o *minioInstallOptions) run(ctx context.Context) error {
	runner := executil.NewRunner()
	client := oc.NewClient(runner)
	if _, err := ensureClusterLogin(ctx, runner, client, ui.IsInteractive()); err != nil {
		return err
	}
	if err := o.defaultAndValidate(ctx, client); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "[object-storage] installing MinIO %s in namespace %s\n", o.name, o.namespace)

	if err := applyObjectStorageManifest(ctx, client, minioNamespaceManifest(o)); err != nil {
		return err
	}
	if err := o.ensureRootSecret(ctx, client); err != nil {
		return err
	}
	for _, manifest := range o.installManifests() {
		if err := applyObjectStorageManifest(ctx, client, manifest); err != nil {
			return err
		}
	}

	if err := waitForDeployment(ctx, client, o.namespace, o.name, o.timeout); err != nil {
		return err
	}
	if _, err := client.RolloutStatus(ctx, o.namespace, "deployment/"+o.name, o.timeout.String()); err != nil {
		return fmt.Errorf("wait for deployment/%s in namespace %s: %w", o.name, o.namespace, err)
	}
	fmt.Fprintf(os.Stdout, "[object-storage] MinIO rollout complete: %s/deployment/%s\n", o.namespace, o.name)

	if err := o.runBucketInitJob(ctx, client); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "[object-storage] Manage buckets ready: %s\n", strings.Join(o.manageBucketNames(), ", "))

	if err := o.enableVirtualHostStyle(ctx, client); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "[object-storage] MinIO virtual-host routing enabled: %s\n", o.virtualHostDomain())

	details, err := o.bucketDetails(ctx, client)
	if err != nil {
		return err
	}
	if o.skipMASConfig {
		if err := applyConnectionDetails(ctx, client, o.namespace, o.minioDetails(details)); err != nil {
			return err
		}
		if err := applyProviderConnectionSecret(ctx, client, o.namespace, s3ConnectionSecretName, "s3", o.s3ConnectionSecretDataMinIO(details)); err != nil {
			return err
		}
		o.printSummary(details)
		return nil
	}

	if err := applyMASObjectStorageConfig(ctx, client, masObjectStorageConfigOptions{
		masInstanceID:      o.masInstanceID,
		masCoreNamespace:   o.masCoreNamespace,
		masCredentialsName: o.masCredentialsName,
		objectStorageCfg:   o.objectStorageCfg,
		displayName:        o.displayName,
		endpointURL:        o.internalEndpointURL(),
		accessKey:          details.AccessKey,
		secretKey:          details.SecretKey,
	}); err != nil {
		return err
	}
	if err := waitForObjectStorageCfgReady(ctx, client, o.masCoreNamespace, o.objectStorageCfg, o.timeout); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "[object-storage] MAS ObjectStorageCfg ready: %s/%s\n", o.masCoreNamespace, o.objectStorageCfg)
	if err := applyConnectionDetails(ctx, client, o.namespace, o.minioDetails(details)); err != nil {
		return err
	}
	if err := applyProviderConnectionSecret(ctx, client, o.namespace, s3ConnectionSecretName, "s3", o.s3ConnectionSecretDataMinIO(details)); err != nil {
		return err
	}
	o.printSummary(details)
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

func (o *minioInstallOptions) defaultAndValidate(ctx context.Context, client *oc.Client) error {
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

	o.namespace = defaultString(o.namespace, defaultMinIONamespace)
	o.name = defaultString(o.name, defaultMinIOName)
	o.bucket = defaultString(o.bucket, defaultMinIOBucket)
	o.storageClassName = strings.TrimSpace(o.storageClassName)
	if o.storageClassName == "" {
		storageClass, err := defaultPVCStorageClass(ctx, client)
		if err != nil {
			return err
		}
		o.storageClassName = storageClass
	}
	o.pvcSize = defaultString(o.pvcSize, defaultMinIOPVCSize)
	o.image = defaultString(o.image, defaultMinIOImage)
	o.mcImage = defaultString(o.mcImage, defaultMinIOMCImage)
	o.rootSecretName = defaultString(o.rootSecretName, o.name+"-root")
	o.masCredentialsName = defaultString(o.masCredentialsName, o.name+"-objectstorage-credentials")
	o.objectStorageCfg = defaultString(o.objectStorageCfg, fmt.Sprintf("ibm-mas-%s-objectstoragecfg-system", o.masInstanceID))
	o.displayName = defaultString(o.displayName, "MAS MinIO Demo Object Storage")
	if o.apiRouteHost == "" {
		host, err := defaultObjectStorageRouteHost(ctx, o.name+"-api")
		if err != nil {
			return err
		}
		o.apiRouteHost = host
	}
	if o.consoleRouteHost == "" {
		host, err := defaultObjectStorageRouteHost(ctx, o.name+"-console")
		if err != nil {
			return err
		}
		o.consoleRouteHost = host
	}
	return nil
}

func defaultPVCStorageClass(ctx context.Context, client *oc.Client) (string, error) {
	storageClasses, err := client.StorageClasses(ctx)
	if err != nil {
		return "", err
	}
	for _, storageClass := range storageClasses {
		if storageClass.IsDefault {
			return storageClass.Name, nil
		}
	}
	for _, storageClass := range storageClasses {
		if storageClass.Name == defaultMinIOStorageClass {
			return storageClass.Name, nil
		}
	}
	if len(storageClasses) > 0 {
		return storageClasses[0].Name, nil
	}
	return "", fmt.Errorf("no storage classes found; provide --storage-class")
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

func (o *minioInstallOptions) installManifests() []map[string]any {
	return []map[string]any{
		minioPVCManifest(o),
		minioServiceManifest(o),
		minioDeploymentManifest(o, false),
		minioRouteManifest(o, o.name+"-api", o.apiRouteHost, "api"),
		minioRouteManifest(o, o.name+"-console", o.consoleRouteHost, "console"),
	}
}

func (o *minioInstallOptions) ensureRootSecret(ctx context.Context, client *oc.Client) error {
	if secret, err := client.SecretData(ctx, o.namespace, o.rootSecretName); err == nil {
		if secret["MINIO_ROOT_USER"] != "" && secret["MINIO_ROOT_PASSWORD"] != "" {
			return nil
		}
		return fmt.Errorf("secret/%s in namespace %s is missing MINIO_ROOT_USER or MINIO_ROOT_PASSWORD", o.rootSecretName, o.namespace)
	}

	password, err := randomSecretString(32)
	if err != nil {
		return err
	}
	manifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"type":       "Opaque",
		"metadata": map[string]any{
			"name":      o.rootSecretName,
			"namespace": o.namespace,
			"labels":    minioLabels(o),
		},
		"stringData": map[string]string{
			"MINIO_ROOT_USER":     "minioadmin",
			"MINIO_ROOT_PASSWORD": password,
		},
	}
	return applyObjectStorageManifest(ctx, client, manifest)
}

func (o *minioInstallOptions) runBucketInitJob(ctx context.Context, client *oc.Client) error {
	jobName := o.name + "-bucket-init"
	if _, err := client.DeleteIgnoreNotFound(ctx, o.namespace, "job/"+jobName); err != nil {
		return err
	}
	if err := applyObjectStorageManifest(ctx, client, minioBucketInitJobManifest(o, jobName)); err != nil {
		return err
	}
	if _, err := client.WaitForCondition(ctx, o.namespace, "job/"+jobName, "complete", o.timeout.String()); err != nil {
		return fmt.Errorf("wait for job/%s in namespace %s: %w", jobName, o.namespace, err)
	}
	return nil
}

func (o *minioInstallOptions) enableVirtualHostStyle(ctx context.Context, client *oc.Client) error {
	for _, bucket := range o.manageBucketNames() {
		if err := applyObjectStorageManifest(ctx, client, minioBucketAliasServiceManifest(o, bucket)); err != nil {
			return err
		}
	}
	if err := applyObjectStorageManifest(ctx, client, minioDeploymentManifest(o, true)); err != nil {
		return err
	}
	if _, err := client.RolloutStatus(ctx, o.namespace, "deployment/"+o.name, o.timeout.String()); err != nil {
		return fmt.Errorf("wait for deployment/%s in namespace %s after enabling virtual-host routing: %w", o.name, o.namespace, err)
	}
	return nil
}

func (o *minioInstallOptions) bucketDetails(ctx context.Context, client *oc.Client) (s3BucketDetails, error) {
	secret, err := client.SecretData(ctx, o.namespace, o.rootSecretName)
	if err != nil {
		return s3BucketDetails{}, err
	}
	accessKey := secret["MINIO_ROOT_USER"]
	secretKey := secret["MINIO_ROOT_PASSWORD"]
	if accessKey == "" || secretKey == "" {
		return s3BucketDetails{}, fmt.Errorf("secret/%s does not contain MinIO root credentials", o.rootSecretName)
	}
	return s3BucketDetails{
		AccessKey: accessKey,
		SecretKey: secretKey,
		Bucket:    o.bucket,
		Host:      o.name + "." + o.namespace + ".svc.cluster.local",
		Port:      "9000",
		Region:    defaultObjectStorageRegion,
	}, nil
}

func (o *minioInstallOptions) internalEndpointURL() string {
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:9000", o.name, o.namespace)
}

func (o *minioInstallOptions) adminEndpointURL() string {
	return fmt.Sprintf("http://%s:9000", o.name)
}

func (o *minioInstallOptions) manageEndpointURL() string {
	return fmt.Sprintf("http://%s.svc.cluster.local:9000", o.namespace)
}

func (o *minioInstallOptions) virtualHostDomain() string {
	return fmt.Sprintf("%s.svc.cluster.local", o.namespace)
}

func (o *minioInstallOptions) manageBucketNames() []string {
	return []string{o.bucket, o.bucket + "recovery", o.bucket + "backup"}
}

func (o *minioInstallOptions) serviceCompatibilityBucketName() string {
	return o.name
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

type masObjectStorageConfigOptions struct {
	masInstanceID      string
	masCoreNamespace   string
	masCredentialsName string
	objectStorageCfg   string
	displayName        string
	endpointURL        string
	accessKey          string
	secretKey          string
	certAlias          string
	caCert             string
}

func (o *objectStorageInstallOptions) applyMASConfig(ctx context.Context, client *oc.Client, details s3BucketDetails, endpointURL, caCert string) error {
	return applyMASObjectStorageConfig(ctx, client, masObjectStorageConfigOptions{
		masInstanceID:      o.masInstanceID,
		masCoreNamespace:   o.masCoreNamespace,
		masCredentialsName: o.masCredentialsName,
		objectStorageCfg:   o.objectStorageCfg,
		displayName:        o.displayName,
		endpointURL:        endpointURL,
		accessKey:          details.AccessKey,
		secretKey:          details.SecretKey,
		certAlias:          o.storeName + "-ca",
		caCert:             caCert,
	})
}

func applyMASObjectStorageConfig(ctx context.Context, client *oc.Client, options masObjectStorageConfigOptions) error {
	credentialSecret := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"type":       "Opaque",
		"metadata": map[string]any{
			"name":      options.masCredentialsName,
			"namespace": options.masCoreNamespace,
			"labels": map[string]string{
				"app.kubernetes.io/instance":   options.masInstanceID,
				"app.kubernetes.io/managed-by": "mas-est-installer",
				"app.kubernetes.io/name":       "mas-est",
				"mas.ibm.com/instanceId":       options.masInstanceID,
			},
		},
		"stringData": map[string]string{
			"username": options.accessKey,
			"password": options.secretKey,
		},
	}
	if err := applyObjectStorageManifest(ctx, client, credentialSecret); err != nil {
		return err
	}

	objectStorageCfg := map[string]any{
		"apiVersion": "config.mas.ibm.com/v1",
		"kind":       "ObjectStorageCfg",
		"metadata": map[string]any{
			"name":      options.objectStorageCfg,
			"namespace": options.masCoreNamespace,
			"labels": map[string]string{
				"app.kubernetes.io/instance":   "ibm-mas",
				"app.kubernetes.io/managed-by": "mas-est-installer",
				"app.kubernetes.io/name":       "ibm-mas",
				"mas.ibm.com/configScope":      "system",
				"mas.ibm.com/instanceId":       options.masInstanceID,
			},
		},
		"spec": map[string]any{
			"displayName": options.displayName,
			"config": map[string]any{
				"url": options.endpointURL,
				"credentials": map[string]string{
					"secretName": options.masCredentialsName,
				},
			},
		},
	}
	if options.caCert != "" {
		certAlias := defaultString(options.certAlias, "object-storage-ca")
		objectStorageCfg["spec"].(map[string]any)["certificates"] = []map[string]string{{
			"alias": certAlias,
			"crt":   options.caCert,
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

func (o *minioInstallOptions) printSummary(details s3BucketDetails) {
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "MinIO S3 details")
	fmt.Fprintf(os.Stdout, "  External S3 API URL: https://%s\n", o.apiRouteHost)
	fmt.Fprintf(os.Stdout, "  MinIO Console URL: https://%s\n", o.consoleRouteHost)
	fmt.Fprintf(os.Stdout, "  Manage endpoint URL: %s\n", o.manageEndpointURL())
	fmt.Fprintf(os.Stdout, "  MAS ObjectStorageCfg endpoint URL: %s\n", o.internalEndpointURL())
	fmt.Fprintf(os.Stdout, "  Bucket: %s\n", details.Bucket)
	fmt.Fprintf(os.Stdout, "  Manage sibling buckets: %s\n", strings.Join(o.manageBucketNames(), ", "))
	fmt.Fprintln(os.Stdout, "  Manage root bucket prefixes: recovery/, backup/")
	fmt.Fprintf(os.Stdout, "  Region: %s\n", details.Region)
	fmt.Fprintf(os.Stdout, "  MinIO root credential secret: %s/%s keys MINIO_ROOT_USER,MINIO_ROOT_PASSWORD\n", o.namespace, o.rootSecretName)
	if !o.skipMASConfig {
		fmt.Fprintf(os.Stdout, "  MAS credential secret: %s/%s keys username,password\n", o.masCoreNamespace, o.masCredentialsName)
		fmt.Fprintf(os.Stdout, "  MAS ObjectStorageCfg: %s/%s\n", o.masCoreNamespace, o.objectStorageCfg)
	}
}

// s3ConnectionSecretData builds the payload for mas-est-s3-connection for
// the Rook Ceph RGW path. Lives in the MAS core namespace alongside the
// ObjectBucketClaim-managed credential secret.
func (o *objectStorageInstallOptions) s3ConnectionSecretData(details s3BucketDetails, endpointURL string) map[string]string {
	return map[string]string{
		"provider":  "rook-ceph-rgw",
		"endpoint":  endpointURL,
		"accessKey": details.AccessKey,
		"secretKey": details.SecretKey,
		"region":    details.Region,
		"bucket":    details.Bucket,
	}
}

func (o *objectStorageInstallOptions) rookDetails(details s3BucketDetails, endpointURL string) map[string]string {
	updates := map[string]string{
		"s3.provider":        "rook-ceph-rgw",
		"s3.endpointURL":     endpointURL,
		"s3.bucket":          details.Bucket,
		"s3.region":          details.Region,
		"s3.accessKeySecret": fmt.Sprintf("%s/%s key AWS_ACCESS_KEY_ID", o.masCoreNamespace, o.bucketClaimName),
		"s3.secretKeySecret": fmt.Sprintf("%s/%s key AWS_SECRET_ACCESS_KEY", o.masCoreNamespace, o.bucketClaimName),
		"s3.namespace":       o.rookNamespace,
		"s3.detailsCommand":  fmt.Sprintf("mas-est details --namespace %s --component s3", o.masCoreNamespace),
	}
	if !o.skipMASConfig {
		updates["s3.masCredentialSecret"] = fmt.Sprintf("%s/%s keys username,password", o.masCoreNamespace, o.masCredentialsName)
		updates["s3.objectStorageCfg"] = fmt.Sprintf("%s/%s", o.masCoreNamespace, o.objectStorageCfg)
	}
	return updates
}

// s3ConnectionSecretDataMinIO builds the payload for mas-est-s3-connection
// for the MinIO path. The endpoint is the in-cluster service URL because the
// AWS SDK in Maximo Manage defaults to virtual-hosted-style addressing and
// the OpenShift route does not support that without a wildcard cert (see
// docs/OBJECT-STORAGE-POC.md "CRITICAL — use the in-cluster URL").
func (o *minioInstallOptions) s3ConnectionSecretDataMinIO(details s3BucketDetails) map[string]string {
	return map[string]string{
		"provider":         "minio",
		"endpoint":         o.internalEndpointURL(),
		"manageEndpoint":   o.manageEndpointURL(),
		"externalEndpoint": "https://" + o.apiRouteHost,
		"consoleUrl":       "https://" + o.consoleRouteHost,
		"accessKey":        details.AccessKey,
		"secretKey":        details.SecretKey,
		"region":           details.Region,
		"bucket":           details.Bucket,
		"siblingBuckets":   strings.Join(o.manageBucketNames(), ","),
	}
}

func (o *minioInstallOptions) minioDetails(details s3BucketDetails) map[string]string {
	updates := map[string]string{
		"s3.provider":             "minio",
		"s3.externalAPIURL":       "https://" + o.apiRouteHost,
		"s3.consoleURL":           "https://" + o.consoleRouteHost,
		"s3.manageEndpointURL":    o.manageEndpointURL(),
		"s3.objectStorageCfgURL":  o.internalEndpointURL(),
		"s3.bucket":               details.Bucket,
		"s3.siblingBuckets":       strings.Join(o.manageBucketNames(), ","),
		"s3.rootBucketPrefixes":   "recovery/,backup/",
		"s3.region":               details.Region,
		"s3.rootCredentialSecret": fmt.Sprintf("%s/%s keys MINIO_ROOT_USER,MINIO_ROOT_PASSWORD", o.namespace, o.rootSecretName),
		"s3.namespace":            o.namespace,
		"s3.detailsCommand":       fmt.Sprintf("mas-est details --namespace %s --component s3", o.namespace),
	}
	if !o.skipMASConfig {
		updates["s3.masCredentialSecret"] = fmt.Sprintf("%s/%s keys username,password", o.masCoreNamespace, o.masCredentialsName)
		updates["s3.objectStorageCfg"] = fmt.Sprintf("%s/%s", o.masCoreNamespace, o.objectStorageCfg)
	}
	return updates
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

func minioNamespaceManifest(o *minioInstallOptions) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name": o.namespace,
			"labels": map[string]string{
				"app.kubernetes.io/managed-by": "mas-est-installer",
				"app.kubernetes.io/name":       "mas-est",
			},
		},
	}
}

func minioPVCManifest(o *minioInstallOptions) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "PersistentVolumeClaim",
		"metadata": map[string]any{
			"name":      o.name + "-data",
			"namespace": o.namespace,
			"labels":    minioLabels(o),
		},
		"spec": map[string]any{
			"accessModes": []string{"ReadWriteOnce"},
			"resources": map[string]any{
				"requests": map[string]string{
					"storage": o.pvcSize,
				},
			},
			"storageClassName": o.storageClassName,
		},
	}
}

func minioServiceManifest(o *minioInstallOptions) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":      o.name,
			"namespace": o.namespace,
			"labels":    minioLabels(o),
		},
		"spec": map[string]any{
			"selector": map[string]string{
				"app.kubernetes.io/instance": o.name,
				"app.kubernetes.io/name":     "minio",
			},
			"ports": []map[string]any{
				{
					"name":       "api",
					"port":       9000,
					"targetPort": 9000,
				},
				{
					"name":       "console",
					"port":       9001,
					"targetPort": 9001,
				},
			},
		},
	}
}

func minioBucketAliasServiceManifest(o *minioInstallOptions, bucket string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":      bucket,
			"namespace": o.namespace,
			"labels":    minioLabels(o),
		},
		"spec": map[string]any{
			"type": "ClusterIP",
			"selector": map[string]string{
				"app.kubernetes.io/instance": o.name,
				"app.kubernetes.io/name":     "minio",
			},
			"ports": []map[string]any{
				{
					"name":       "api",
					"port":       9000,
					"protocol":   "TCP",
					"targetPort": 9000,
				},
			},
		},
	}
}

func minioDeploymentManifest(o *minioInstallOptions, enableVirtualHostStyle bool) map[string]any {
	labels := minioLabels(o)
	env := []map[string]any{
		{
			"name": "MINIO_ROOT_USER",
			"valueFrom": map[string]any{
				"secretKeyRef": map[string]string{
					"name": o.rootSecretName,
					"key":  "MINIO_ROOT_USER",
				},
			},
		},
		{
			"name": "MINIO_ROOT_PASSWORD",
			"valueFrom": map[string]any{
				"secretKeyRef": map[string]string{
					"name": o.rootSecretName,
					"key":  "MINIO_ROOT_PASSWORD",
				},
			},
		},
	}
	if enableVirtualHostStyle {
		env = append(env, map[string]any{
			"name":  "MINIO_DOMAIN",
			"value": o.virtualHostDomain(),
		})
	}

	return map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      o.name,
			"namespace": o.namespace,
			"labels":    labels,
		},
		"spec": map[string]any{
			"replicas": 1,
			"selector": map[string]any{
				"matchLabels": map[string]string{
					"app.kubernetes.io/instance": o.name,
					"app.kubernetes.io/name":     "minio",
				},
			},
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": labels,
				},
				"spec": map[string]any{
					"containers": []map[string]any{
						{
							"name":  "minio",
							"image": o.image,
							"args":  []string{"server", "/data", "--console-address", ":9001"},
							"env":   env,
							"ports": []map[string]any{
								{
									"name":          "api",
									"containerPort": 9000,
								},
								{
									"name":          "console",
									"containerPort": 9001,
								},
							},
							"livenessProbe": map[string]any{
								"httpGet": map[string]any{
									"path": "/minio/health/live",
									"port": 9000,
								},
								"initialDelaySeconds": 10,
								"periodSeconds":       20,
							},
							"readinessProbe": map[string]any{
								"httpGet": map[string]any{
									"path": "/minio/health/ready",
									"port": 9000,
								},
								"initialDelaySeconds": 10,
								"periodSeconds":       10,
							},
							"volumeMounts": []map[string]string{
								{
									"name":      "data",
									"mountPath": "/data",
								},
							},
						},
					},
					"volumes": []map[string]any{
						{
							"name": "data",
							"persistentVolumeClaim": map[string]string{
								"claimName": o.name + "-data",
							},
						},
					},
				},
			},
		},
	}
}

func minioRouteManifest(o *minioInstallOptions, name, host, targetPort string) map[string]any {
	return map[string]any{
		"apiVersion": "route.openshift.io/v1",
		"kind":       "Route",
		"metadata": map[string]any{
			"name":      name,
			"namespace": o.namespace,
			"labels":    minioLabels(o),
		},
		"spec": map[string]any{
			"host": host,
			"to": map[string]string{
				"kind": "Service",
				"name": o.name,
			},
			"port": map[string]string{
				"targetPort": targetPort,
			},
			"tls": map[string]string{
				"termination":                   "edge",
				"insecureEdgeTerminationPolicy": "Redirect",
			},
		},
	}
}

func minioBucketInitJobManifest(o *minioInstallOptions, jobName string) map[string]any {
	return map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]any{
			"name":      jobName,
			"namespace": o.namespace,
			"labels":    minioLabels(o),
		},
		"spec": map[string]any{
			"backoffLimit": 6,
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": minioLabels(o),
				},
				"spec": map[string]any{
					"restartPolicy": "OnFailure",
					"containers": []map[string]any{
						{
							"name":  "mc",
							"image": o.mcImage,
							"env": []map[string]any{
								{
									"name":  "HOME",
									"value": "/tmp",
								},
								{
									"name":  "MC_CONFIG_DIR",
									"value": "/tmp/.mc",
								},
								{
									"name": "MINIO_ROOT_USER",
									"valueFrom": map[string]any{
										"secretKeyRef": map[string]string{
											"name": o.rootSecretName,
											"key":  "MINIO_ROOT_USER",
										},
									},
								},
								{
									"name": "MINIO_ROOT_PASSWORD",
									"valueFrom": map[string]any{
										"secretKeyRef": map[string]string{
											"name": o.rootSecretName,
											"key":  "MINIO_ROOT_PASSWORD",
										},
									},
								},
								{
									"name":  "MINIO_BUCKET",
									"value": o.bucket,
								},
								{
									"name":  "MINIO_MANAGE_BUCKETS",
									"value": strings.Join(o.manageBucketNames(), " "),
								},
								{
									"name":  "MINIO_SERVICE_BUCKET",
									"value": o.serviceCompatibilityBucketName(),
								},
							},
							"command": []string{"/bin/sh", "-c"},
							"args": []string{
								fmt.Sprintf("set -eu\nmkdir -p \"$MC_CONFIG_DIR\"\nuntil mc alias set local %q \"$MINIO_ROOT_USER\" \"$MINIO_ROOT_PASSWORD\"; do sleep 5; done\nfor bucket in $MINIO_MANAGE_BUCKETS; do\n  mc mb --ignore-existing \"local/$bucket\"\ndone\nmc mb --ignore-existing \"local/$MINIO_SERVICE_BUCKET\"\nempty=/tmp/mas-est-empty\n: > \"$empty\"\nmc cp \"$empty\" \"local/$MINIO_BUCKET/recovery/.keep\"\nmc cp \"$empty\" \"local/$MINIO_BUCKET/backup/.keep\"\nmc ls local\n", o.adminEndpointURL()),
							},
						},
					},
				},
			},
		},
	}
}

func minioLabels(o *minioInstallOptions) map[string]string {
	return map[string]string{
		"app.kubernetes.io/instance":   o.name,
		"app.kubernetes.io/managed-by": "mas-est-installer",
		"app.kubernetes.io/name":       "minio",
		"mas.ibm.com/instanceId":       o.masInstanceID,
	}
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

func randomSecretString(byteCount int) (string, error) {
	raw := make([]byte, byteCount)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate random secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
