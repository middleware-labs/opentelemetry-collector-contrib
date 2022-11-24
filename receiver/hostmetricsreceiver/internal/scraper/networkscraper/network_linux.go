// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build linux
// +build linux

package networkscraper // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver/internal/scraper/networkscraper"

import (
	"fmt"
	"time"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver/internal/scraper/networkscraper/bcal"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver/internal/scraper/networkscraper/internal/metadata"
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

func (s *scraper) recordNetworkConntrackMetrics() error {
	if !s.config.Metrics.SystemNetworkConntrackCount.Enabled && !s.config.Metrics.SystemNetworkConntrackMax.Enabled {
		return nil
	}

	now := pcommon.NewTimestampFromTime(time.Now())
	conntrack, err := s.conntrack()
	if err != nil {
		return fmt.Errorf("failed to read conntrack info: %w", err)
	}
	s.mb.RecordSystemNetworkConntrackCountDataPoint(now, conntrack[0].ConnTrackCount)
	s.mb.RecordSystemNetworkConntrackMaxDataPoint(now, conntrack[0].ConnTrackMax)
	return nil
}

func (s *scraper) recordSystemNetworkIoBandwidth(now pcommon.Timestamp, networkBandwidth bcal.NetworkBandwidth) {
	if s.config.Metrics.SystemNetworkIoBandwidth.Enabled {
		s.mb.RecordSystemNetworkIoBandwidthDataPoint(now, networkBandwidth.InboundRate, metadata.AttributeDirectionReceive)
		s.mb.RecordSystemNetworkIoBandwidthDataPoint(now, networkBandwidth.OutboundRate, metadata.AttributeDirectionTransmit)
	}
}
