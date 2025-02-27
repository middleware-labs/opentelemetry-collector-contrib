// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package networkscraper // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver/internal/scraper/networkscraper"

import (
	"context"
	"fmt"
	"time"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver/internal/scraper/networkscraper/bcal"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver/internal/scraper/networkscraper/internal/metadata"
	"github.com/shirou/gopsutil/v4/common"
	"go.opentelemetry.io/collector/pdata/pcommon"
)

var allTCPStates = []string{
	"CLOSE_WAIT",
	"CLOSE",
	"CLOSING",
	"DELETE",
	"ESTABLISHED",
	"FIN_WAIT_1",
	"FIN_WAIT_2",
	"LAST_ACK",
	"LISTEN",
	"SYN_SENT",
	"SYN_RECV",
	"TIME_WAIT",
}

func (s *networkScraper) recordNetworkConntrackMetrics() error {
	if !s.config.MetricsBuilderConfig.Metrics.SystemNetworkConntrackCount.Enabled && !s.config.MetricsBuilderConfig.Metrics.SystemNetworkConntrackMax.Enabled {
		return nil
	}
	ctx := context.WithValue(context.Background(), common.EnvKey, s.config.EnvMap)
	now := pcommon.NewTimestampFromTime(time.Now())
	conntrack, err := s.conntrack(ctx)
	if err != nil {
		return fmt.Errorf("failed to read conntrack info: %w", err)
	}
	s.mb.RecordSystemNetworkConntrackCountDataPoint(now, conntrack[0].ConnTrackCount)
	s.mb.RecordSystemNetworkConntrackMaxDataPoint(now, conntrack[0].ConnTrackMax)
	return nil
}

func (s *networkScraper) recordSystemNetworkIoBandwidth(now pcommon.Timestamp, networkBandwidthMap map[string]bcal.NetworkBandwidth) {
	if s.config.MetricsBuilderConfig.Metrics.SystemNetworkIoBandwidth.Enabled {
		for device, networkBandwidth := range networkBandwidthMap {
			s.mb.RecordSystemNetworkIoBandwidthDataPoint(now, networkBandwidth.InboundRate, device, metadata.AttributeDirectionReceive)
			s.mb.RecordSystemNetworkIoBandwidthDataPoint(now, networkBandwidth.OutboundRate, device, metadata.AttributeDirectionTransmit)
		}
	}
}
