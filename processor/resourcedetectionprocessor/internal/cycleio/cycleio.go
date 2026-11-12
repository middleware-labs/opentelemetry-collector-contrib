// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package env provides a detector that loads resource information from
// the OTEL_RESOURCE environment variable. A list of labels of the form
// `<key1>=<value1>,<key2>=<value2>,...` is accepted. Domain names and
// paths are accepted as label keys.
package cycleio // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourcedetectionprocessor/internal/cycleio"

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/processor"
	conventions "go.opentelemetry.io/otel/semconv/v1.40.0"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourcedetectionprocessor/internal"
)

// TypeStr is type of detector.
const (
	TypeStr              = "cycleio"
	providerVendorEnvVar = "CYCLE_PROVIDER_VENDOR"
	providerLocation     = "CYCLE_PROVIDER_LOCATION"
	hostnameEnvVar       = "CYCLE_SERVER_ID"
	clusterEnvVar        = "CYCLE_CLUSTER"

	cycleAPITokenEnvVar    = "CYCLE_API_TOKEN"
	cycleAPIUnixSocket     = "/var/run/cycle/api/api.sock"
	cycleAPIHost           = "localhost"
	cycleAPIServerEndpoint = "/v1/server"

	cycleTokenHeader = "X-CYCLE-TOKEN"
)

var _ internal.Detector = (*Detector)(nil)

type cycleProvider struct {
	Vendor   string `json:"vendor"`
	Model    string `json:"model"`
	Location string `json:"location"`
	Zone     string `json:"zone"`
	Server   string `json:"server"`
	InitIPs  []any  `json:"init_ips"`
}

type cycleServerData struct {
	ID       string        `json:"id"`
	Hostname string        `json:"hostname"`
	Provider cycleProvider `json:"provider"`
	Cluster  string        `json:"cluster"`
}

type cycleServerInfo struct {
	Data cycleServerData `json:"data"`
}

type Detector struct{}

func NewDetector(processor.Settings, internal.DetectorConfig, bool) (internal.Detector, error) {
	return &Detector{}, nil
}

func (d *Detector) Detect(context.Context) (resource pcommon.Resource, schemaURL string, err error) {
	res := pcommon.NewResource()

	res.Attributes().PutStr(string(conventions.OSTypeKey), conventions.OSTypeLinux.Value.AsString())
	cycleAPIToken := os.Getenv(cycleAPITokenEnvVar)
	if cycleAPIToken != "" {
		serverInfo, err := getServerInfo(cycleAPIToken)
		if err != nil {
			return res, "", err
		}
		data := serverInfo.Data

		res.Attributes().PutStr(string(conventions.CloudProviderKey), getCloudProvider(data.Provider.Vendor))
		res.Attributes().PutStr(string(conventions.CloudRegionKey), data.Provider.Location)
		res.Attributes().PutStr(string(conventions.CloudAvailabilityZoneKey), data.Provider.Zone)
		res.Attributes().PutStr(string(conventions.HostIDKey), data.ID)
		res.Attributes().PutStr(string(conventions.HostNameKey), data.Hostname)
		res.Attributes().PutStr(string(conventions.HostTypeKey), data.Provider.Model)
		res.Attributes().PutEmptySlice("host.ip").FromRaw(data.Provider.InitIPs)

		res.Attributes().PutStr("cycle.cluster.id", data.Cluster)
	} else {

		vendor := os.Getenv(providerVendorEnvVar)
		if vendor == "" {
			vendor = "unknown"
		}
		res.Attributes().PutStr(string(conventions.CloudProviderKey), getCloudProvider(vendor))

		region := os.Getenv(providerLocation)
		if region == "" {
			region = "unknown"
		}
		res.Attributes().PutStr(string(conventions.CloudRegionKey), region)

		hostID := os.Getenv(hostnameEnvVar)
		if hostID == "" {
			hostID = "cycleio-server"
		}
		res.Attributes().PutStr(string(conventions.HostIDKey), hostID)
		res.Attributes().PutStr(string(conventions.HostNameKey), hostID)

		cluster := os.Getenv(clusterEnvVar)
		if cluster == "" {
			cluster = "unknown"
		}
		res.Attributes().PutStr("cycle.cluster.id", cluster)
	}

	return res, "", nil
}

func getCloudProvider(provider string) string {
	switch provider {
	case "aws":
		return conventions.CloudProviderAWS.Value.AsString()
	case "gcp":
		return conventions.CloudProviderGCP.Value.AsString()
	case "azure":
		return conventions.CloudProviderAzure.Value.AsString()
	default:
		return provider
	}
}

func getServerInfo(token string) (*cycleServerInfo, error) {
	var serverInfo cycleServerInfo

	// Create a custom HTTP transport that uses the Unix socket
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", cycleAPIUnixSocket)
		},
	}

	// Create an HTTP client with the custom transport
	client := &http.Client{
		Transport: transport,
	}

	// Construct the request URL
	u := &url.URL{
		Scheme: "http",
		Host:   cycleAPIHost, // This is ignored but required for forming a valid URL
		Path:   cycleAPIServerEndpoint,
	}

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return &serverInfo, err
	}
	req.Header.Add(cycleTokenHeader, token)
	resp, err := client.Do(req)
	if err != nil {
		return &serverInfo, err
	}

	defer resp.Body.Close()

	// Read and print the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &serverInfo, err
	}

	fmt.Println("###", string(body))
	err = json.Unmarshal(body, &serverInfo)
	if err != nil {
		return &serverInfo, err
	}

	fmt.Println("###", serverInfo)
	return &serverInfo, nil
}
