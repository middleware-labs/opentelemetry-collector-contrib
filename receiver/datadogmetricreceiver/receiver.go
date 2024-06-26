// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogmetricreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver"

import (
	"compress/gzip"
	"compress/zlib"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/receiverhelper"

	metricsV2 "github.com/DataDog/agent-payload/v5/gogen"
	processv1 "github.com/DataDog/agent-payload/v5/process"
	metricsV1 "github.com/DataDog/datadog-api-client-go/v2/api/datadogV1"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/cluster"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/clusterrolebinding"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/clusterroles"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/cronjob"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/daemonset"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/deployment"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/helpers"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/hpa"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/ingress"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/job"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/namespace"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/node"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/persistentvolume"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/persistentvolumeclaim"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/pod"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/replicaset"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/rolebinding"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/roles"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/service"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/serviceaccount"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/statefulset"
)

const (
	datadogMetricTypeCount = int32(metricsV2.MetricPayload_COUNT)
	datadogMetricTypeGauge = int32(metricsV2.MetricPayload_GAUGE)
	datadogMetricTypeRate  = int32(metricsV2.MetricPayload_RATE)

	datadogAPIKeyHeader = "Dd-Api-Key"
)

type datadogmetricreceiver struct {
	address      string
	config       *Config
	params       receiver.CreateSettings
	nextConsumer consumer.Metrics
	server       *http.Server
	tReceiver    *receiverhelper.ObsReport
}

type hostMetadata struct {
	// from gohai/cpu
	CPUCores             uint64  `json:"cpu_cores"`
	CPULogicalProcessors uint64  `json:"cpu_logical_processors"`
	CPUVendor            string  `json:"cpu_vendor"`
	CPUModel             string  `json:"cpu_model"`
	CPUModelID           string  `json:"cpu_model_id"`
	CPUFamily            string  `json:"cpu_family"`
	CPUStepping          string  `json:"cpu_stepping"`
	CPUFrequency         float64 `json:"cpu_frequency"`
	CPUCacheSize         uint64  `json:"cpu_cache_size"`

	// from gohai/platform
	KernelName      string `json:"kernel_name"`
	KernelRelease   string `json:"kernel_release"`
	KernelVersion   string `json:"kernel_version"`
	OS              string `json:"os"`
	CPUArchitecture string `json:"cpu_architecture"`

	// from gohai/memory
	MemoryTotalKb     uint64 `json:"memory_total_kb"`
	MemorySwapTotalKb uint64 `json:"memory_swap_total_kb"`

	// from gohai/network
	IPAddress   string `json:"ip_address"`
	IPv6Address string `json:"ipv6_address"`
	MacAddress  string `json:"mac_address"`

	// from the agent itself
	AgentVersion           string `json:"agent_version"`
	CloudProvider          string `json:"cloud_provider"`
	CloudProviderSource    string `json:"cloud_provider_source"`
	CloudProviderAccountID string `json:"cloud_provider_account_id"`
	CloudProviderHostID    string `json:"cloud_provider_host_id"`
	OsVersion              string `json:"os_version"`

	// from file system
	HypervisorGuestUUID string `json:"hypervisor_guest_uuid"`
	DmiProductUUID      string `json:"dmi_product_uuid"`
	DmiBoardAssetTag    string `json:"dmi_board_asset_tag"`
	DmiBoardVendor      string `json:"dmi_board_vendor"`

	// from package repositories
	LinuxPackageSigningEnabled   bool `json:"linux_package_signing_enabled"`
	RPMGlobalRepoGPGCheckEnabled bool `json:"rpm_global_repo_gpg_check_enabled"`
}

type MetaDataPayload struct {
	Hostname  string        `json:"hostname"`
	Timestamp int64         `json:"timestamp"`
	Metadata  *hostMetadata `json:"host_metadata"`
	UUID      string        `json:"uuid"`
}

type IntakePayload struct {
	GohaiPayload  string            `json:"gohai"`
	Meta          *Meta             `json:"meta"`
	ContainerMeta map[string]string `json:"container-meta,omitempty"`
}

type Meta struct {
	SocketHostname string   `json:"socket-hostname"`
	Timezones      []string `json:"timezones"`
	SocketFqdn     string   `json:"socket-fqdn"`
	EC2Hostname    string   `json:"ec2-hostname"`
	Hostname       string   `json:"hostname"`
	HostAliases    []string `json:"host_aliases"`
	InstanceID     string   `json:"instance-id"`
	AgentHostname  string   `json:"agent-hostname,omitempty"`
	ClusterName    string   `json:"cluster-name,omitempty"`
}

type GoHaiData struct {
	FileSystem []FileInfo `json:"filesystem"`
}

type FileInfo struct {
	KbSize    string `json:"kb_size"`
	MountedOn string `json:"mounted_on"`
	Name      string `json:"name"`
}

func newdatadogmetricreceiver(config *Config, nextConsumer consumer.Metrics, params receiver.CreateSettings) (receiver.Metrics, error) {

	instance, err := receiverhelper.NewObsReport(receiverhelper.ObsReportSettings{LongLivedCtx: false, ReceiverID: params.ID, Transport: "http", ReceiverCreateSettings: params})
	if err != nil {
		return nil, err
	}

	return &datadogmetricreceiver{
		params:       params,
		config:       config,
		nextConsumer: nextConsumer,
		server: &http.Server{
			ReadTimeout: config.ReadTimeout,
		},
		tReceiver: instance,
	}, nil
}

func (ddr *datadogmetricreceiver) Start(ctx context.Context, host component.Host) error {
	ddmux := http.NewServeMux()
	ddmux.HandleFunc("/api/v2/series", ddr.handleV2Series)
	ddmux.HandleFunc("/api/v1/metadata", ddr.handleMetaData)
	ddmux.HandleFunc("/intake/", ddr.handleIntake)
	ddmux.HandleFunc("/api/v1/validate", ddr.handleValidate)
	ddmux.HandleFunc("/api/v1/series", ddr.handleV2Series)
	ddmux.HandleFunc("/api/v1/collector", ddr.handleCollector)
	ddmux.HandleFunc("/api/v1/check_run", ddr.handleCheckRun)
	ddmux.HandleFunc("/api/v1/connections", ddr.handleConnections)
	ddmux.HandleFunc("/api/v2/orch", ddr.handleOrchestrator)
	// Not Implemented Handlers
	ddmux.HandleFunc("/api/v1/sketches", ddr.handleNotImplemenetedAPI)
	ddmux.HandleFunc("/api/v2/host_metadata", ddr.handleNotImplemenetedAPI)
	ddmux.HandleFunc("/api/v2/events", ddr.handleNotImplemenetedAPI)
	ddmux.HandleFunc("/api/v2/service_checks", ddr.handleNotImplemenetedAPI)
	ddmux.HandleFunc("/api/beta/sketches", ddr.handleNotImplemenetedAPI)
	ddmux.HandleFunc("/api/v1/discovery", ddr.handleNotImplemenetedAPI)
	ddmux.HandleFunc("/api/v2/proclcycle", ddr.handleNotImplemenetedAPI)
	ddmux.HandleFunc("/api/v1/container", ddr.handleNotImplemenetedAPI)
	ddmux.HandleFunc("/api/v1/orchestrator", ddr.handleNotImplemenetedAPI)
	ddmux.HandleFunc("/api/v2/orchmanif", ddr.handleNotImplemenetedAPI)

	var err error
	ddr.server, err = ddr.config.ServerConfig.ToServer(
		ctx,
		host,
		ddr.params.TelemetrySettings,
		ddmux,
	)
	if err != nil {
		return fmt.Errorf("failed to create server definition: %w", err)
	}
	hln, err := ddr.config.ServerConfig.ToListener(ctx)
	if err != nil {
		return fmt.Errorf("failed to create datadog listener: %w", err)
	}

	ddr.address = hln.Addr().String()

	go func() {
		if err := ddr.server.Serve(hln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			ddr.params.TelemetrySettings.ReportStatus(component.NewFatalErrorEvent(fmt.Errorf("error starting datadog receiver: %w", err)))
		}
	}()
	return nil
}

func (ddr *datadogmetricreceiver) Shutdown(ctx context.Context) (err error) {
	return ddr.server.Shutdown(ctx)
}

func readAndCloseBody(resp http.ResponseWriter, req *http.Request) ([]byte, bool) {
	// Check if the request body is compressed
	var reader io.Reader = req.Body
	if strings.Contains(req.Header.Get("Content-Encoding"), "gzip") {
		// Decompress gzip
		gz, err := gzip.NewReader(req.Body)
		if err != nil {
			fmt.Println("err", err)
			//            return
		}
		defer gz.Close()
		reader = gz
	} else if strings.Contains(req.Header.Get("Content-Encoding"), "deflate") {
		// Decompress deflate
		zlibReader, err := zlib.NewReader(req.Body)
		if err != nil {
			fmt.Println("err", err)
			// return
		}
		defer zlibReader.Close()
		reader = zlibReader
	}

	body, err := io.ReadAll(reader)
	if err != nil {
		fmt.Println("err", err)
		return nil, false
	}
	if err = req.Body.Close(); err != nil {
		fmt.Println("err", err)
		return nil, false
	}
	return body, true
}

func (ddr *datadogmetricreceiver) handleV2Series(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")
	key := req.Header.Get(datadogAPIKeyHeader)
	body, ok := readAndCloseBody(w, req)
	if !ok {
		http.Error(w, "error in reading request body", http.StatusBadRequest)
		return
	}

	var otlpReq pmetricotlp.ExportRequest
	var err error
	// is the Datadog agent using V1 endpoint ? Datadog V1 uses json input
	// and slightly different payload structure.
	if strings.HasPrefix(req.URL.Path, "/api/v1") {
		var v1Metrics metricsV1.MetricsPayload
		err = json.Unmarshal(body, &v1Metrics)
		if err != nil {
			http.Error(w, "error in unmarshalling json", http.StatusBadRequest)
			return
		}

		if len(v1Metrics.GetSeries()) == 0 {
			http.Error(w, "no metrics in the payload", http.StatusBadRequest)
			return
		}

		// convert datadog V1 metrics to Otel format
		otlpReq, err = getOtlpExportReqFromDatadogV1Metrics(origin, key, v1Metrics)
	} else {
		// datadog agent is sending us V2 payload which using protobuf
		var v2Metrics metricsV2.MetricPayload
		err = v2Metrics.Unmarshal(body)
		if err != nil {
			http.Error(w, "error in unmarshalling req payload", http.StatusBadRequest)
			return
		}
		otlpReq, err = GetOtlpExportReqFromDatadogV2Metrics(origin, key, v2Metrics)
	}

	if err != nil {
		http.Error(w, "Metrics consumer errored out", http.StatusInternalServerError)
		return
	}
	obsCtx := ddr.tReceiver.StartLogsOp(req.Context())
	errs := ddr.nextConsumer.ConsumeMetrics(obsCtx, otlpReq.Metrics())
	if errs != nil {
		http.Error(w, "Logs consumer errored out", http.StatusInternalServerError)
		ddr.params.Logger.Error("Logs consumer errored out")
	} else {
		_, _ = w.Write([]byte("OK"))
	}
}

func (ddr *datadogmetricreceiver) handleIntake(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")
	key := req.Header.Get(datadogAPIKeyHeader)

	body, ok := readAndCloseBody(w, req)
	if !ok {
		http.Error(w, "error in reading request body", http.StatusBadRequest)
		return
	}
	var otlpReq pmetricotlp.ExportRequest

	var err error
	var intake IntakePayload
	if err = json.Unmarshal(body, &intake); err != nil {
		fmt.Println("error unmarshalling intake payload:", err)
		http.Error(w, "error in unmarshaling json", http.StatusBadRequest)
		return
	}

	// Unmarshal Gohai FileDatapayload from IntakePayload
	var gohai GoHaiData
	if err = json.Unmarshal([]byte(intake.GohaiPayload), &gohai); err != nil {
		http.Error(w, "error in unmarshaling json", http.StatusBadRequest)
		return
	}

	if intake.Meta.Hostname == "" {
		http.Error(w, "HostName not found", http.StatusBadRequest)
		return
	}

	hostname := intake.Meta.Hostname

	otlpReq, err = getOtlpExportReqFromDatadogIntakeData(origin, key, gohai, struct {
		hostname      string
		containerInfo map[string]string
		milliseconds  int64
	}{
		hostname:      hostname,
		containerInfo: intake.ContainerMeta,
		milliseconds:  (time.Now().UnixNano() / int64(time.Millisecond)) * 1000000,
	})

	if err != nil {
		http.Error(w, "error in metadata getOtlpExportReqFromDatadogV1MetaData", http.StatusBadRequest)
		return
	}
	obsCtx := ddr.tReceiver.StartLogsOp(req.Context())
	errs := ddr.nextConsumer.ConsumeMetrics(obsCtx, otlpReq.Metrics())
	if errs != nil {
		http.Error(w, "Logs consumer errored out", http.StatusInternalServerError)
		ddr.params.Logger.Error("Logs consumer errored out")
	} else {
		_, _ = w.Write([]byte("OK"))
	}
}

func (ddr *datadogmetricreceiver) handleCheckRun(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	fmt.Fprintf(w, `{"status":"ok"}`)
}
func (ddr *datadogmetricreceiver) handleValidate(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"valid":true}`)
}
func (ddr *datadogmetricreceiver) handleMetaData(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")
	key := req.Header.Get(datadogAPIKeyHeader)
	body, ok := readAndCloseBody(w, req)
	if !ok {
		http.Error(w, "error in reading request body", http.StatusBadRequest)
		return
	}
	var otlpReq pmetricotlp.ExportRequest
	var metadataPayload MetaDataPayload
	var err error
	err = json.Unmarshal(body, &metadataPayload)

	if err != nil {
		http.Error(w, "error in unmarshaling json", http.StatusBadRequest)
		return
	}
	otlpReq, err = getOtlpExportReqFromDatadogV1MetaData(origin, key, metadataPayload)

	if err != nil {
		http.Error(w, "error in metadata getOtlpExportReqFromDatadogV1MetaData", http.StatusBadRequest)
		return
	}
	obsCtx := ddr.tReceiver.StartLogsOp(req.Context())
	errs := ddr.nextConsumer.ConsumeMetrics(obsCtx, otlpReq.Metrics())
	if errs != nil {
		http.Error(w, "Logs consumer errored out", http.StatusInternalServerError)
		ddr.params.Logger.Error("Logs consumer errored out")
	} else {
		_, _ = w.Write([]byte("OK"))
	}
}

func (ddr *datadogmetricreceiver) handleConnections(w http.ResponseWriter, req *http.Request) {
	// TODO Implement translation flow if any connection related info required in future
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"valid":true}`)
}

func (ddr *datadogmetricreceiver) handleCollector(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")
	key := req.Header.Get(datadogAPIKeyHeader)
	body, ok := readAndCloseBody(w, req)
	if !ok {
		http.Error(w, "error in reading request body", http.StatusBadRequest)
		return
	}
	var err error
	// Decode the message
	reqBody, err := processv1.DecodeMessage(body)
	if err != nil {
		http.Error(w, "error in decoding request body", http.StatusBadRequest)
		return
	}

	collectorProc, ok := reqBody.Body.(*processv1.CollectorProc)
	if !ok {
		http.Error(w, "error in unmarshalling collector", http.StatusBadRequest)
		return
	}

	var otlpReq pmetricotlp.ExportRequest

	otlpReq, err = getOtlpExportReqFromDatadogProcessesData(origin, key, collectorProc)

	if err != nil {
		http.Error(w, "error in getOtlpExportReqFromDatadogProcessesData", http.StatusBadRequest)
		return
	}

	obsCtx := ddr.tReceiver.StartLogsOp(req.Context())
	errs := ddr.nextConsumer.ConsumeMetrics(obsCtx, otlpReq.Metrics())
	if errs != nil {
		http.Error(w, "Logs consumer errored out", http.StatusInternalServerError)
		ddr.params.Logger.Error("Logs consumer errored out")
	} else {
		_, _ = w.Write([]byte("OK"))
	}
}

func (ddr *datadogmetricreceiver) handleOrchestrator(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")
	key := req.Header.Get(datadogAPIKeyHeader)
	body, ok := readAndCloseBody(w, req)
	if !ok {
		http.Error(w, "error in reading request body", http.StatusBadRequest)
		return
	}
	var err error

	reqBody, err := processv1.DecodeMessage(body)
	if err != nil {
		http.Error(w, "error in decoding request body", http.StatusBadRequest)
		return
	}

	timestamp := reqBody.Header.Timestamp
	resourceType := reqBody.Header.Type

	if timestamp == 0 {
		timestamp = helpers.GetMillis()
	}

	var otlpReq pmetricotlp.ExportRequest

	switch resourceType {
	case processv1.TypeCollectorRoleBinding:
		otlpReq, err = rolebinding.GetOtlpExportReqFromDatadogRoleBindingData(origin, key, reqBody.Body, timestamp)
	case processv1.TypeCollectorClusterRoleBinding:
		otlpReq, err = clusterrolebinding.GetOtlpExportReqFromDatadogClusterRoleBindingData(origin, key, reqBody.Body, timestamp)
	case processv1.TypeCollectorRole:
		otlpReq, err = roles.GetOtlpExportReqFromDatadogRolesData(origin, key, reqBody.Body, timestamp)
	case processv1.TypeCollectorClusterRole:
		otlpReq, err = clusterroles.GetOtlpExportReqFromDatadogClusterRolesData(origin, key, reqBody.Body, timestamp)
	case processv1.TypeCollectorCluster:
		otlpReq, err = cluster.GetOtlpExportReqFromClusterData(origin, key, reqBody.Body, timestamp)
	case processv1.TypeCollectorNamespace:
		otlpReq, err = namespace.GetOtlpExportReqFromNamespaceData(origin, key, reqBody.Body, timestamp)
	case processv1.TypeCollectorServiceAccount:
		otlpReq, err = serviceaccount.GetOtlpExportReqFromDatadogServiceAccountData(origin, key, reqBody.Body, timestamp)
	case processv1.TypeCollectorPersistentVolumeClaim:
		otlpReq, err = persistentvolumeclaim.GetOtlpExportReqFromDatadogPVCData(origin, key, reqBody.Body, timestamp)
	case processv1.TypeCollectorPersistentVolume:
		otlpReq, err = persistentvolume.GetOtlpExportReqFromDatadogPVData(origin, key, reqBody.Body, timestamp)
	case processv1.TypeCollectorHorizontalPodAutoscaler:
		otlpReq, err = hpa.GetOtlpExportReqFromDatadogHPAData(origin, key, reqBody.Body, timestamp)
	case processv1.TypeCollectorIngress:
		otlpReq, err = ingress.GetOtlpExportReqFromDatadogIngressData(origin, key, reqBody.Body, timestamp)
	case processv1.TypeCollectorJob:
		otlpReq, err = job.GetOtlpExportReqFromDatadogJobData(origin, key, reqBody.Body, timestamp)
	case processv1.TypeCollectorCronJob:
		otlpReq, err = cronjob.GetOtlpExportReqFromDatadogCronJobData(origin, key, reqBody.Body, timestamp)
	case processv1.TypeCollectorReplicaSet:
		otlpReq, err = replicaset.GetOtlpExportReqFromDatadogReplicaSetData(origin, key, reqBody.Body, timestamp)
	case processv1.TypeCollectorStatefulSet:
		otlpReq, err = statefulset.GetOtlpExportReqFromDatadogStatefulSetData(origin, key, reqBody.Body, timestamp)
	case processv1.TypeCollectorService:
		otlpReq, err = service.GetOtlpExportReqFromDatadogServiceData(origin, key, reqBody.Body, timestamp)
	case processv1.TypeCollectorDaemonSet:
		otlpReq, err = daemonset.GetOtlpExportReqFromDatadogDaemonSetData(origin, key, reqBody.Body, timestamp)
	case processv1.TypeCollectorDeployment:
		otlpReq, err = deployment.GetOtlpExportReqFromDatadogDeploymentData(origin, key, reqBody.Body, timestamp)
	case processv1.TypeCollectorNode:
		otlpReq, err = node.GetOtlpExportReqFromDatadogNodeData(origin, key, reqBody.Body, timestamp)
	case processv1.TypeCollectorPod:
		otlpReq, err = pod.GetOtlpExportReqFromPodData(origin, key, reqBody.Body, timestamp)
	default:
		http.Error(w, "unsupported message type", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, "error in getOtlpExportReqFromDatadogProcessesData", http.StatusBadRequest)
		return
	}

	obsCtx := ddr.tReceiver.StartLogsOp(req.Context())
	errs := ddr.nextConsumer.ConsumeMetrics(obsCtx, otlpReq.Metrics())
	if errs != nil {
		http.Error(w, "Logs consumer errored out", http.StatusInternalServerError)
		ddr.params.Logger.Error("Logs consumer errored out")
	} else {
		_, _ = w.Write([]byte("OK"))
	}
}

func (ddr *datadogmetricreceiver) handleNotImplemenetedAPI(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"valid":true}`)
}
