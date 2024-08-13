package apachedruidreceiver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/receiverhelper"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/apachedruidreceiver/internal/metadata"
)

type ApacheDruidMetricReceiver struct {
	address      string
	config       *Config
	params       receiver.CreateSettings
	nextConsumer consumer.Metrics
	server       *http.Server
	tReceiver    *receiverhelper.ObsReport
	OtelMetadata metadata.Metrics
}

func NewApacheDruidMetricReceiver(
	config *Config,
	nextConsumer consumer.Metrics,
	params receiver.CreateSettings,
) (receiver.Metrics, error) {
	instance, err := receiverhelper.NewObsReport(receiverhelper.ObsReportSettings{
		LongLivedCtx:           false,
		ReceiverID:             params.ID,
		Transport:              "http",
		ReceiverCreateSettings: params,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create ObsReport: %w", err)
	}

	filename := "metadata.yaml"
	yamlData, err := metadata.ReadYamlFile(filename)
	if err != nil {
		return nil, fmt.Errorf("error reading YAML file: %v", err)
	}

	metricsMetadata, err := metadata.ParseMetrics(yamlData)

	if err != nil {
		return nil, fmt.Errorf("failed to load Druid metrics: %w", err)
	}

	return &ApacheDruidMetricReceiver{
		params:       params,
		config:       config,
		nextConsumer: nextConsumer,
		server: &http.Server{
			ReadTimeout: config.ReadTimeout,
		},
		OtelMetadata: metricsMetadata,
		tReceiver:    instance,
	}, nil
}

// Start implements receiver.Metrics.
func (adr *ApacheDruidMetricReceiver) Start(ctx context.Context, host component.Host) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", adr.handleMetrics)

	var err error

	adr.server, err = adr.config.ServerConfig.ToServer(
		ctx,
		host,
		adr.params.TelemetrySettings,
		mux,
	)
	if err != nil {
		return fmt.Errorf("failed to create server definition: %w", err)
	}
	hln, err := adr.config.ServerConfig.ToListener(ctx)

	if err != nil {
		return fmt.Errorf("failed to create druid listener: %w", err)
	}

	adr.address = hln.Addr().String()

	go func() {
		if err := adr.server.Serve(hln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			adr.params.TelemetrySettings.ReportStatus(component.NewFatalErrorEvent(fmt.Errorf("error starting datadog receiver: %w", err)))
		}
	}()

	return nil
}

// Shutdown implements receiver.Metrics.
func (adr *ApacheDruidMetricReceiver) Shutdown(ctx context.Context) error {
	return adr.server.Shutdown(ctx)
}

func (adr *ApacheDruidMetricReceiver) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read the raw body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Unable to read request body", http.StatusBadRequest)
		return
	}

	var payloadMetrics []map[string]interface{}
	if err := json.Unmarshal(body, &payloadMetrics); err != nil {
		log.Printf("Error parsing JSON: %v", err)
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Validate the payload
	if err := validatePayload(payloadMetrics); err != nil {
		log.Printf("Invalid payload: %v", err)
		http.Error(w, fmt.Sprintf("Invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	otlpReq, err := getOtlpExportReqFromDruidMetrics(
		payloadMetrics,
		adr.OtelMetadata,
	)
	if err != nil {
		http.Error(w, "Metrics consumer errored out", http.StatusInternalServerError)
		return
	}

	obsCtx := adr.tReceiver.StartLogsOp(r.Context())
	errs := adr.nextConsumer.ConsumeMetrics(obsCtx, otlpReq.Metrics())
	if errs != nil {
		http.Error(w, "Logs consumer errored out", http.StatusInternalServerError)
		adr.params.Logger.Error("Logs consumer errored out")
	} else {
		_, _ = w.Write([]byte("OK"))
	}
}

func validatePayload(payload []map[string]interface{}) error {
	if len(payload) == 0 {
		return fmt.Errorf("payload is empty")
	}

	for i, metric := range payload {
		if _, ok := metric["metric"]; !ok {
			return fmt.Errorf("metric %d is missing 'metric' field", i)
		}
		if _, ok := metric["value"]; !ok {
			return fmt.Errorf("metric %d is missing 'value' field", i)
		}

		// Optional: Check if value is a number (float64 or int)
		switch metric["value"].(type) {
		case float64, int, int64:
			// These types are okay
		default:
			return fmt.Errorf("metric %d has non-numeric 'value' field", i)
		}
	}

	return nil
}
