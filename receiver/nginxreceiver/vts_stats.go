package nginxreceiver

import (
	"encoding/json"
	"fmt"
	"io"
)

type NginxVtsStatus struct {
	HostName      string        `json:"hostName"`
	ModuleVersion string        `json:"moduleVersion"`
	NginxVersion  string        `json:"nginxVersion"`
	LoadMsec      int64         `json:"loadMsec"`
	NowMsec       int64         `json:"nowMsec"`
	Connections   Connections   `json:"connections"`
	SharedZones   SharedZones   `json:"sharedZones"`
	ServerZones   ServerZones   `json:"serverZones"`
	FilterZones   FilterZones   `json:"filterZones"`
	UpstreamZones UpstreamZones `json:"upstreamZones"`
	CacheZones    CacheZones    `json:"cacheZones"`
}

type Connections struct {
	Active   int64 `json:"active"`
	Reading  int64 `json:"reading"`
	Writing  int64 `json:"writing"`
	Waiting  int64 `json:"waiting"`
	Accepted int64 `json:"accepted"`
	Handled  int64 `json:"handled"`
	Requests int64 `json:"requests"`
}

type SharedZones struct {
	Name     string `json:"name"`
	MaxSize  int64  `json:"maxSize"`
	UsedSize int64  `json:"usedSize"`
	UsedNode int64  `json:"usedNode"`
}

type Responses struct {
	Status1xx   int64 `json:"1xx"`
	Status2xx   int64 `json:"2xx"`
	Status3xx   int64 `json:"3xx"`
	Status4xx   int64 `json:"4xx"`
	Status5xx   int64 `json:"5xx"`
	Miss        int64 `json:"miss"`
	Bypass      int64 `json:"bypass"`
	Expired     int64 `json:"expired"`
	Stale       int64 `json:"stale"`
	Updating    int64 `json:"updating"`
	Revalidated int64 `json:"revalidated"`
	Hit         int64 `json:"hit"`
	Scarce      int64 `json:"scarce"`
}

type RequestMetrics struct {
	Times []int64 `json:"times"`
	Msecs []int64 `json:"msecs"`
}

type RequestBuckets struct {
	Msecs    []int64 `json:"msecs"`
	Counters []int64 `json:"counters"`
}

type ZoneStats struct {
	RequestCounter     int64          `json:"requestCounter"`
	InBytes            int64          `json:"inBytes"`
	OutBytes           int64          `json:"outBytes"`
	Responses          Responses      `json:"responses"`
	RequestMsecCounter int64          `json:"requestMsecCounter"`
	RequestMsec        int64          `json:"requestMsec"`
	RequestMsecs       RequestMetrics `json:"requestMsecs"`
	RequestBuckets     RequestBuckets `json:"requestBuckets"`
}

type ServerZones map[string]ZoneStats

type FilterZones map[string]map[string]ZoneStats

type UpstreamServer struct {
	ZoneStats
	Server              string         `json:"server"`
	ResponseMsecCounter int64          `json:"responseMsecCounter"`
	ResponseMsec        int64          `json:"responseMsec"`
	ResponseMsecs       RequestMetrics `json:"responseMsecs"`
	ResponseBuckets     RequestBuckets `json:"responseBuckets"`
	Weight              int            `json:"weight"`
	MaxFails            int            `json:"maxFails"`
	FailTimeout         int            `json:"failTimeout"`
	Backup              bool           `json:"backup"`
	Down                bool           `json:"down"`
}

type UpstreamZones map[string][]UpstreamServer

type CacheZoneStats struct {
	MaxSize   int64     `json:"maxSize"`
	UsedSize  int64     `json:"usedSize"`
	InBytes   int64     `json:"inBytes"`
	OutBytes  int64     `json:"outBytes"`
	Responses Responses `json:"responses"`
}

type CacheZones map[string]CacheZoneStats

func ParseVtsStats(r io.Reader) (*NginxVtsStatus, error) {
	decoder := json.NewDecoder(r)

	// Create a map to store raw JSON first
	var rawData map[string]interface{}
	if err := decoder.Decode(&rawData); err != nil {
		return nil, fmt.Errorf("failed to decode JSON: %w", err)
	}

	// Marshal back to JSON bytes to ensure proper handling of numeric types
	jsonBytes, err := json.Marshal(rawData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal intermediate JSON: %w", err)
	}

	// Unmarshal into our structured type
	var stats NginxVtsStatus
	if err := json.Unmarshal(jsonBytes, &stats); err != nil {
		return nil, fmt.Errorf("failed to unmarshal into NginxStatus: %w", err)
	}

	// Validate required fields
	if err := validateStats(&stats); err != nil {
		return nil, fmt.Errorf("stats validation failed: %w", err)
	}

	return &stats, nil
}

func validateStats(stats *NginxVtsStatus) error {
	if stats == nil {
		return fmt.Errorf("stats cannot be nil")
	}

	// Validate required string fields
	if stats.HostName == "" {
		return fmt.Errorf("hostName is required")
	}
	if stats.ModuleVersion == "" {
		return fmt.Errorf("moduleVersion is required")
	}
	if stats.NginxVersion == "" {
		return fmt.Errorf("nginxVersion is required")
	}

	// Validate time fields
	if stats.LoadMsec <= 0 {
		return fmt.Errorf("loadMsec must be positive")
	}
	if stats.NowMsec <= 0 {
		return fmt.Errorf("nowMsec must be positive")
	}

	// Validate connections
	if stats.Connections.Handled < 0 ||
		stats.Connections.Accepted < 0 ||
		stats.Connections.Active < 0 ||
		stats.Connections.Requests < 0 {
		return fmt.Errorf("connection counts cannot be negative")
	}

	// Basic validation of zones
	if len(stats.ServerZones) == 0 {
		return fmt.Errorf("serverZones cannot be empty")
	}

	return nil
}
