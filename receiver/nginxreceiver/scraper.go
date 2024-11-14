// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package nginxreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/nginxreceiver"

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver"
	"go.uber.org/zap"

	"github.com/k0kubun/pp"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/nginxreceiver/internal/metadata"
)

type nginxScraper struct {
	httpClient *http.Client
	client     *NginxClient

	settings component.TelemetrySettings
	cfg      *Config
	mb       *metadata.MetricsBuilder
}

func newNginxScraper(
	settings receiver.Settings,
	cfg *Config,
) *nginxScraper {
	mb := metadata.NewMetricsBuilder(cfg.MetricsBuilderConfig, settings)
	return &nginxScraper{
		settings: settings.TelemetrySettings,
		cfg:      cfg,
		mb:       mb,
	}
}

func (r *nginxScraper) start(ctx context.Context, host component.Host) error {
	httpClient, err := r.cfg.ToClient(ctx, host, r.settings)
	if err != nil {
		return err
	}
	r.httpClient = httpClient

	return nil
}

func (r *nginxScraper) scrape(context.Context) (pmetric.Metrics, error) {
	// Init client in scrape method in case there are transient errors in the constructor.
	if r.client == nil {
		var err error
		r.client, err = NewNginxClient(r.httpClient, r.cfg.ClientConfig.Endpoint, r.cfg.VTSEndpoint)

		if err != nil {
			r.client = nil
			return pmetric.Metrics{}, err
		}
	}

	stats, err := r.client.GetStubStats()
	if err != nil {
		r.settings.Logger.Error("Failed to fetch nginx stats", zap.Error(err))
		return pmetric.Metrics{}, err
	}

	vtsStats, err := r.client.GetVtsStats()

	if err != nil {
		r.settings.Logger.Error("Failed to fetch nginx stats", zap.Error(err))
		return pmetric.Metrics{}, err
	}

	// pp.Println(vtsStats)

	now := pcommon.NewTimestampFromTime(time.Now())

	r.recordVtsStats(now, vtsStats)

	r.mb.RecordNginxRequestsDataPoint(now, stats.Requests)
	r.mb.RecordNginxConnectionsAcceptedDataPoint(now, stats.Connections.Accepted)
	r.mb.RecordNginxConnectionsHandledDataPoint(now, stats.Connections.Handled)
	r.mb.RecordNginxConnectionsCurrentDataPoint(now, stats.Connections.Active, metadata.AttributeStateActive)
	r.mb.RecordNginxConnectionsCurrentDataPoint(now, stats.Connections.Reading, metadata.AttributeStateReading)
	r.mb.RecordNginxConnectionsCurrentDataPoint(now, stats.Connections.Writing, metadata.AttributeStateWriting)
	r.mb.RecordNginxConnectionsCurrentDataPoint(now, stats.Connections.Waiting, metadata.AttributeStateWaiting)

	return r.mb.Emit(), nil
}

func (r *nginxScraper) recordVtsStats(now pcommon.Timestamp, vtsStats *NginxVtsStatus) {
	r.recordTimingStats(now, vtsStats)
	r.recordVtsConnectionStats(now, vtsStats)
}

func (r *nginxScraper) recordVtsConnectionStats(now pcommon.Timestamp, vtsStats *NginxVtsStatus) {
	r.mb.RecordNginxNetReadingDataPoint(now, vtsStats.Connections.Reading)
	r.mb.RecordNginxNetWritingDataPoint(now, vtsStats.Connections.Writing)
	r.mb.RecordNginxNetWaitingDataPoint(now, vtsStats.Connections.Waiting)
}

func (r *nginxScraper) recordTimingStats(now pcommon.Timestamp, vtsStats *NginxVtsStatus) {

	// r.mb.RecordNginxLoadTimestampDataPoint(now, vtsStats.LoadMsec)

	for upstreamZones, v := range vtsStats.UpstreamZones {
		for _, val := range v {
			pp.Println(val.Server)
			pp.Println(val.ResponseMsec)

			r.mb.RecordNginxUpstreamPeersResponseTimeDataPoint(
				now, val.ResponseMsec, upstreamZones, val.Server,
			)
		}
	}
}
