// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package networkscraper // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver/internal/scraper/networkscraper"

import (
	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver/internal/scraper/networkscraper/bcal"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver/internal/scraper/networkscraper/internal/metadata"
)

var allTCPStates = []string{
	"CLOSE_WAIT",
	"CLOSED",
	"CLOSING",
	"DELETE",
	"ESTABLISHED",
	"FIN_WAIT_1",
	"FIN_WAIT_2",
	"LAST_ACK",
	"LISTEN",
	"SYN_SENT",
	"SYN_RECEIVED",
	"TIME_WAIT",
}

func (s *networkScraper) recordNetworkConntrackMetrics() error {
	return nil
}

func (s *networkScraper) recordSystemNetworkIoBandwidth(now pcommon.Timestamp, networkBandwidthMap map[string]bcal.NetworkBandwidth) {
	if s.config.Metrics.SystemNetworkIoBandwidth.Enabled {
		for device, networkBandwidth := range networkBandwidthMap {
			s.mb.RecordSystemNetworkIoBandwidthDataPoint(now, networkBandwidth.InboundRate, device, metadata.AttributeDirectionReceive)
			s.mb.RecordSystemNetworkIoBandwidthDataPoint(now, networkBandwidth.OutboundRate, device, metadata.AttributeDirectionTransmit)
		}
	}
}
