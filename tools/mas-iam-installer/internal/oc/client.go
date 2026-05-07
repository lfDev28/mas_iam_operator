package oc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	executil "github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/exec"
)

type Client struct {
	runner *executil.Runner
}

type StorageClass struct {
	Name      string
	IsDefault bool
}

type RouteRef struct {
	Namespace string
	Name      string
	Host      string
}

type MASRoute struct {
	InstanceID string
	Namespace  string
	Name       string
	Host       string
}

type ManageWorkspace struct {
	Name        string
	Namespace   string
	InstanceID  string
	WorkspaceID string
}

func (r MASRoute) SCIMBaseURL() string {
	return fmt.Sprintf("https://%s/scim/v2", r.Host)
}

type CSVStatus struct {
	Name  string
	Phase string
}

type DeploymentStatus struct {
	Name              string
	DesiredReplicas   int32
	ReadyReplicas     int32
	AvailableReplicas int32
}

type PodStatus struct {
	Name            string
	Phase           string
	ReadyContainers int
	TotalContainers int
}

type JobStatus struct {
	Name      string
	Active    int32
	Succeeded int32
	Failed    int32
	Complete  bool
}

type PVCStatus struct {
	Name         string
	Phase        string
	StorageClass string
}

var masCoreNamespacePattern = regexp.MustCompile(`^mas-(.+)-core$`)

func NewClient(runner *executil.Runner) *Client {
	return &Client{runner: runner}
}

func (c *Client) CheckAvailable() error {
	_, err := c.runner.LookPath("oc")
	if err != nil {
		return fmt.Errorf("oc not found on PATH")
	}
	return nil
}

func (c *Client) WhoAmI(ctx context.Context) (string, string, error) {
	userOutput, err := c.runner.Output(ctx, executil.Options{Name: "oc", Args: []string{"whoami"}})
	if err != nil {
		return "", "", fmt.Errorf("cluster login check failed; run 'oc login' first: %w", err)
	}
	serverOutput, err := c.runner.Output(ctx, executil.Options{Name: "oc", Args: []string{"whoami", "--show-server"}})
	if err != nil {
		return "", "", fmt.Errorf("unable to determine current cluster server; run 'oc login' first: %w", err)
	}
	return strings.TrimSpace(userOutput), strings.TrimSpace(serverOutput), nil
}

func (c *Client) StorageClasses(ctx context.Context) ([]StorageClass, error) {
	output, err := c.runner.Output(ctx, executil.Options{Name: "oc", Args: []string{"get", "sc", "-o", "json"}})
	if err != nil {
		return nil, err
	}

	var payload struct {
		Items []struct {
			Metadata struct {
				Name        string            `json:"name"`
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return nil, fmt.Errorf("decode storage classes: %w", err)
	}

	classes := make([]StorageClass, 0, len(payload.Items))
	for _, item := range payload.Items {
		isDefault := item.Metadata.Annotations["storageclass.kubernetes.io/is-default-class"] == "true" ||
			item.Metadata.Annotations["storageclass.beta.kubernetes.io/is-default-class"] == "true"
		classes = append(classes, StorageClass{
			Name:      item.Metadata.Name,
			IsDefault: isDefault,
		})
	}
	return classes, nil
}

func (c *Client) RoutesByHost(ctx context.Context, host string) ([]RouteRef, error) {
	output, err := c.runner.Output(ctx, executil.Options{Name: "oc", Args: []string{"get", "route", "-A", "-o", "json"}})
	if err != nil {
		return nil, err
	}

	var payload struct {
		Items []struct {
			Metadata struct {
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
			} `json:"metadata"`
			Spec struct {
				Host string `json:"host"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return nil, fmt.Errorf("decode routes: %w", err)
	}

	matches := []RouteRef{}
	for _, item := range payload.Items {
		if item.Spec.Host != host {
			continue
		}
		matches = append(matches, RouteRef{
			Namespace: item.Metadata.Namespace,
			Name:      item.Metadata.Name,
			Host:      item.Spec.Host,
		})
	}
	return matches, nil
}

func (c *Client) MASRoutes(ctx context.Context) ([]MASRoute, error) {
	output, err := c.runner.Output(ctx, executil.Options{Name: "oc", Args: []string{"get", "route", "-A", "-o", "json"}})
	if err != nil {
		return nil, err
	}

	var payload struct {
		Items []struct {
			Metadata struct {
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
			} `json:"metadata"`
			Spec struct {
				Host string `json:"host"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return nil, fmt.Errorf("decode routes: %w", err)
	}

	routes := []MASRoute{}
	for _, item := range payload.Items {
		matches := masCoreNamespacePattern.FindStringSubmatch(item.Metadata.Namespace)
		if len(matches) != 2 {
			continue
		}

		instanceID := matches[1]
		expectedRouteName := instanceID + "-api"
		if item.Metadata.Name != expectedRouteName {
			continue
		}

		routes = append(routes, MASRoute{
			InstanceID: instanceID,
			Namespace:  item.Metadata.Namespace,
			Name:       item.Metadata.Name,
			Host:       item.Spec.Host,
		})
	}

	sort.Slice(routes, func(i, j int) bool {
		if routes[i].InstanceID != routes[j].InstanceID {
			return routes[i].InstanceID < routes[j].InstanceID
		}
		if routes[i].Namespace != routes[j].Namespace {
			return routes[i].Namespace < routes[j].Namespace
		}
		return routes[i].Name < routes[j].Name
	})

	return routes, nil
}

func (c *Client) ManageWorkspaces(ctx context.Context) ([]ManageWorkspace, error) {
	output, err := c.runner.Output(ctx, executil.Options{Name: "oc", Args: []string{"get", "manageworkspaces.apps.mas.ibm.com", "-A", "-o", "json"}})
	if err != nil {
		output, err = c.runner.Output(ctx, executil.Options{Name: "oc", Args: []string{"get", "manageworkspaces", "-A", "-o", "json"}})
		if err != nil {
			return nil, err
		}
	}

	var payload struct {
		Items []struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return nil, fmt.Errorf("decode manageworkspaces: %w", err)
	}

	workspaces := []ManageWorkspace{}
	for _, item := range payload.Items {
		instanceID, workspaceID, ok := strings.Cut(item.Metadata.Name, "-")
		if !ok || strings.TrimSpace(instanceID) == "" || strings.TrimSpace(workspaceID) == "" {
			continue
		}
		workspaces = append(workspaces, ManageWorkspace{
			Name:        item.Metadata.Name,
			Namespace:   item.Metadata.Namespace,
			InstanceID:  instanceID,
			WorkspaceID: workspaceID,
		})
	}

	sort.Slice(workspaces, func(i, j int) bool {
		if workspaces[i].InstanceID != workspaces[j].InstanceID {
			return workspaces[i].InstanceID < workspaces[j].InstanceID
		}
		if workspaces[i].WorkspaceID != workspaces[j].WorkspaceID {
			return workspaces[i].WorkspaceID < workspaces[j].WorkspaceID
		}
		return workspaces[i].Name < workspaces[j].Name
	})

	return workspaces, nil
}

func (c *Client) CSVPhase(ctx context.Context, namespace string) (CSVStatus, error) {
	if csvName, err := c.subscriptionCurrentCSV(ctx, namespace); err == nil && csvName != "" {
		return c.csvStatusByName(ctx, namespace, csvName)
	}

	label := fmt.Sprintf("operators.coreos.com/mas-iam-operator.%s", namespace)
	output, err := c.runner.Output(ctx, executil.Options{
		Name: "oc",
		Args: []string{"get", "csv", "-n", namespace, "-l", label, "-o", "json"},
	})
	if err != nil {
		return CSVStatus{}, err
	}

	var payload struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return CSVStatus{}, fmt.Errorf("decode csv list: %w", err)
	}
	if len(payload.Items) > 0 {
		return CSVStatus{
			Name:  payload.Items[0].Metadata.Name,
			Phase: payload.Items[0].Status.Phase,
		}, nil
	}

	if csvName, err := c.findCSVByNamePrefix(ctx, namespace, "mas-iam-operator.v"); err == nil && csvName != "" {
		return c.csvStatusByName(ctx, namespace, csvName)
	}

	return CSVStatus{}, fmt.Errorf("csv not found in namespace %s", namespace)
}

func (c *Client) subscriptionCurrentCSV(ctx context.Context, namespace string) (string, error) {
	output, err := c.runner.Output(ctx, executil.Options{
		Name: "oc",
		Args: []string{"get", "subscription.operators.coreos.com", "mas-iam-operator", "-n", namespace, "-o", "json"},
	})
	if err != nil {
		return "", err
	}

	var payload struct {
		Status struct {
			CurrentCSV string `json:"currentCSV"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return "", fmt.Errorf("decode subscription mas-iam-operator: %w", err)
	}

	return payload.Status.CurrentCSV, nil
}

func (c *Client) csvStatusByName(ctx context.Context, namespace, name string) (CSVStatus, error) {
	output, err := c.runner.Output(ctx, executil.Options{
		Name: "oc",
		Args: []string{"get", "csv", name, "-n", namespace, "-o", "json"},
	})
	if err != nil {
		return CSVStatus{}, err
	}

	var payload struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Status struct {
			Phase string `json:"phase"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return CSVStatus{}, fmt.Errorf("decode csv %s: %w", name, err)
	}

	return CSVStatus{
		Name:  payload.Metadata.Name,
		Phase: payload.Status.Phase,
	}, nil
}

func (c *Client) findCSVByNamePrefix(ctx context.Context, namespace, prefix string) (string, error) {
	output, err := c.runner.Output(ctx, executil.Options{
		Name: "oc",
		Args: []string{"get", "csv", "-n", namespace, "-o", "json"},
	})
	if err != nil {
		return "", err
	}

	var payload struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return "", fmt.Errorf("decode csv list: %w", err)
	}

	for _, item := range payload.Items {
		if strings.HasPrefix(item.Metadata.Name, prefix) {
			return item.Metadata.Name, nil
		}
	}

	return "", nil
}

func (c *Client) Deployment(ctx context.Context, namespace, name string) (DeploymentStatus, error) {
	output, err := c.runner.Output(ctx, executil.Options{
		Name: "oc",
		Args: []string{"get", "deployment", name, "-n", namespace, "-o", "json"},
	})
	if err != nil {
		return DeploymentStatus{}, err
	}

	var payload struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Spec struct {
			Replicas *int32 `json:"replicas"`
		} `json:"spec"`
		Status struct {
			ReadyReplicas     int32 `json:"readyReplicas"`
			AvailableReplicas int32 `json:"availableReplicas"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return DeploymentStatus{}, fmt.Errorf("decode deployment %s: %w", name, err)
	}

	desired := int32(1)
	if payload.Spec.Replicas != nil {
		desired = *payload.Spec.Replicas
	}

	return DeploymentStatus{
		Name:              payload.Metadata.Name,
		DesiredReplicas:   desired,
		ReadyReplicas:     payload.Status.ReadyReplicas,
		AvailableReplicas: payload.Status.AvailableReplicas,
	}, nil
}

func (c *Client) Pod(ctx context.Context, namespace, name string) (PodStatus, error) {
	output, err := c.runner.Output(ctx, executil.Options{
		Name: "oc",
		Args: []string{"get", "pod", name, "-n", namespace, "-o", "json"},
	})
	if err != nil {
		return PodStatus{}, err
	}

	var payload struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Status struct {
			Phase             string `json:"phase"`
			ContainerStatuses []struct {
				Ready bool `json:"ready"`
			} `json:"containerStatuses"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return PodStatus{}, fmt.Errorf("decode pod %s: %w", name, err)
	}

	ready := 0
	for _, status := range payload.Status.ContainerStatuses {
		if status.Ready {
			ready++
		}
	}

	return PodStatus{
		Name:            payload.Metadata.Name,
		Phase:           payload.Status.Phase,
		ReadyContainers: ready,
		TotalContainers: len(payload.Status.ContainerStatuses),
	}, nil
}

func (c *Client) Job(ctx context.Context, namespace, name string) (JobStatus, error) {
	output, err := c.runner.Output(ctx, executil.Options{
		Name: "oc",
		Args: []string{"get", "job", name, "-n", namespace, "-o", "json"},
	})
	if err != nil {
		return JobStatus{}, err
	}

	var payload struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Status struct {
			Active     int32 `json:"active"`
			Succeeded  int32 `json:"succeeded"`
			Failed     int32 `json:"failed"`
			Conditions []struct {
				Type   string `json:"type"`
				Status string `json:"status"`
			} `json:"conditions"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return JobStatus{}, fmt.Errorf("decode job %s: %w", name, err)
	}

	complete := false
	for _, condition := range payload.Status.Conditions {
		if condition.Type == "Complete" && condition.Status == "True" {
			complete = true
			break
		}
	}

	return JobStatus{
		Name:      payload.Metadata.Name,
		Active:    payload.Status.Active,
		Succeeded: payload.Status.Succeeded,
		Failed:    payload.Status.Failed,
		Complete:  complete,
	}, nil
}

func (c *Client) PVCs(ctx context.Context, namespace string) ([]PVCStatus, error) {
	output, err := c.runner.Output(ctx, executil.Options{
		Name: "oc",
		Args: []string{"get", "pvc", "-n", namespace, "-o", "json"},
	})
	if err != nil {
		return nil, err
	}

	var payload struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Spec struct {
				StorageClassName string `json:"storageClassName"`
			} `json:"spec"`
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return nil, fmt.Errorf("decode pvc list: %w", err)
	}

	pvcs := make([]PVCStatus, 0, len(payload.Items))
	for _, item := range payload.Items {
		pvcs = append(pvcs, PVCStatus{
			Name:         item.Metadata.Name,
			Phase:        item.Status.Phase,
			StorageClass: item.Spec.StorageClassName,
		})
	}
	return pvcs, nil
}

func (c *Client) ConfigMapData(ctx context.Context, namespace, name string) (map[string]string, error) {
	output, err := c.runner.Output(ctx, executil.Options{
		Name: "oc",
		Args: []string{"get", "configmap", name, "-n", namespace, "-o", "json"},
	})
	if err != nil {
		return nil, err
	}

	var payload struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return nil, fmt.Errorf("decode configmap %s: %w", name, err)
	}
	if payload.Data == nil {
		return map[string]string{}, nil
	}
	return payload.Data, nil
}

func (c *Client) SecretJSON(ctx context.Context, namespace, name string) ([]byte, error) {
	output, err := c.runner.Output(ctx, executil.Options{
		Name: "oc",
		Args: []string{"get", "secret", name, "-n", namespace, "-o", "json"},
	})
	if err != nil {
		return nil, err
	}
	return []byte(output), nil
}

func (c *Client) SecretKeys(ctx context.Context, namespace, name string) ([]string, error) {
	raw, err := c.SecretJSON(ctx, namespace, name)
	if err != nil {
		return nil, err
	}

	var payload struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode secret %s: %w", name, err)
	}

	keys := make([]string, 0, len(payload.Data))
	for key := range payload.Data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}

func (c *Client) SecretData(ctx context.Context, namespace, name string) (map[string]string, error) {
	output, err := c.runner.Output(ctx, executil.Options{
		Name: "oc",
		Args: []string{"get", "secret", name, "-n", namespace, "-o", "json"},
	})
	if err != nil {
		return nil, err
	}

	var payload struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return nil, fmt.Errorf("decode secret %s: %w", name, err)
	}

	decoded := map[string]string{}
	for key, value := range payload.Data {
		raw, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return nil, fmt.Errorf("decode secret %s key %s: %w", name, key, err)
		}
		decoded[key] = string(raw)
	}
	return decoded, nil
}

func (c *Client) Patch(ctx context.Context, namespace, target string, patch []byte) (string, error) {
	return c.runner.InputOutput(ctx, executil.Options{
		Name: "oc",
		Args: []string{"patch", target, "-n", namespace, "--type", "merge", "--patch-file", "/dev/stdin"},
	}, bytes.NewReader(patch))
}

func (c *Client) RolloutRestart(ctx context.Context, namespace, target string) (string, error) {
	return c.runner.Output(ctx, executil.Options{
		Name: "oc",
		Args: []string{"rollout", "restart", target, "-n", namespace},
	})
}

func (c *Client) RolloutStatus(ctx context.Context, namespace, target, timeout string) (string, error) {
	return c.runner.Output(ctx, executil.Options{
		Name: "oc",
		Args: []string{"rollout", "status", target, "-n", namespace, "--timeout=" + timeout},
	})
}

func (c *Client) Logs(ctx context.Context, namespace string, args []string, stdout, stderr io.Writer) error {
	fullArgs := []string{"logs"}
	fullArgs = append(fullArgs, args...)
	fullArgs = append(fullArgs, "-n", namespace)
	return c.runner.Stream(ctx, executil.Options{
		Name: "oc",
		Args: fullArgs,
	}, stdout, stderr)
}

func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	value := strings.ToLower(err.Error())
	return strings.Contains(value, "notfound") || strings.Contains(value, "not found")
}
