// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ecsagent // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/ecsagent"

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	defaultMetadataEndpoint = "http://169.254.169.254/latest/meta-data/local-ipv4"
	ecsAgentMetadataPath    = "http://%s:51678/v1/metadata"
	ecsAgentTasksPath       = "http://%s:51678/v1/tasks"
	defaultHTTPTimeout      = 5 * time.Second
)

// ECSAgentTask represents a task from the ECS agent introspection API.
type ECSAgentTask struct {
	ARN         string              `json:"Arn"`
	Family      string              `json:"Family"`
	Version     string              `json:"Version"`
	KnownStatus string              `json:"KnownStatus"`
	Containers  []ECSAgentContainer `json:"Containers"`
}

// ECSAgentContainer represents a container in an ECS agent task.
type ECSAgentContainer struct {
	DockerID   string `json:"DockerId"`
	DockerName string `json:"DockerName"`
	Name       string `json:"Name"`
}

// ECSAgentTasksResponse is the response from /v1/tasks.
type ECSAgentTasksResponse struct {
	Tasks []ECSAgentTask `json:"Tasks"`
}

// ECSAgentMetadataResponse is the response from /v1/metadata.
type ECSAgentMetadataResponse struct {
	Cluster                 string `json:"Cluster"`
	ContainerInstanceARN    string `json:"ContainerInstanceArn"`
	EC2InstanceID           string `json:"EC2InstanceId"`
	ContainerInstanceRegion string `json:"ContainerInstanceRegion"`
}

// Client fetches task and metadata from the ECS agent introspection API.
type Client interface {
	GetTasks(ctx context.Context, instanceIP string) ([]ECSAgentTask, error)
	GetMetadata(ctx context.Context, instanceIP string) (*ECSAgentMetadataResponse, error)
}

// InstanceIPProvider provides the EC2 instance's primary private IP.
type InstanceIPProvider interface {
	GetInstanceIP(ctx context.Context) (string, error)
}

type ecsAgentClient struct {
	httpClient *http.Client
	logger     *zap.Logger
}

// NewClient creates an ECS agent HTTP client.
func NewClient(logger *zap.Logger) Client {
	return &ecsAgentClient{
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
		logger:     logger,
	}
}

func (c *ecsAgentClient) GetTasks(ctx context.Context, instanceIP string) ([]ECSAgentTask, error) {
	url := fmt.Sprintf(ecsAgentTasksPath, instanceIP)
	resp, err := c.doRequest(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("ecs agent tasks request failed: %w", err)
	}
	defer resp.Body.Close()

	var out ECSAgentTasksResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode ecs agent tasks response: %w", err)
	}
	return out.Tasks, nil
}

func (c *ecsAgentClient) GetMetadata(ctx context.Context, instanceIP string) (*ECSAgentMetadataResponse, error) {
	url := fmt.Sprintf(ecsAgentMetadataPath, instanceIP)
	resp, err := c.doRequest(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("ecs agent metadata request failed: %w", err)
	}
	defer resp.Body.Close()

	var out ECSAgentMetadataResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode ecs agent metadata response: %w", err)
	}
	return &out, nil
}

func (c *ecsAgentClient) doRequest(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("ecs agent returned status %d: %s", resp.StatusCode, string(body))
	}
	return resp, nil
}

// EC2MetadataInstanceIPProvider gets instance IP from EC2 instance metadata service.
type EC2MetadataInstanceIPProvider struct {
	httpClient *http.Client
}

// NewEC2MetadataInstanceIPProvider creates a provider that reads from EC2 metadata.
func NewEC2MetadataInstanceIPProvider() InstanceIPProvider {
	return &EC2MetadataInstanceIPProvider{
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
	}
}

// GetInstanceIP returns the instance's primary private IPv4.
func (p *EC2MetadataInstanceIPProvider) GetInstanceIP(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, defaultMetadataEndpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ec2 metadata returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(body))
	if ip == "" {
		return "", fmt.Errorf("empty instance IP from metadata")
	}
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("invalid instance IP: %s", ip)
	}
	return ip, nil
}
