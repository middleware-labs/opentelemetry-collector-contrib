package profile

// Inventory columns that no profile declares. The Datadog agent hardcodes the
// same tables in pkg/collector/corechecks/snmp/internal/checkconfig/
// config_metadata.go (LegacyMetadataConfig "ip_addresses",
// TopologyMetadataConfig, VPNTunnelMetadataConfig, RouteMetadataConfig and
// TunnelMetadataConfig); the OIDs below are the same.

// IP-MIB ipAddrTable.
const (
	OIDIPAdEntIfIndex = "1.3.6.1.2.1.4.20.1.2"
	OIDIPAdEntNetMask = "1.3.6.1.2.1.4.20.1.3"
)

// LLDP-MIB. lldpRemTable is indexed by lldpRemTimeMark.lldpRemLocalPortNum.
// lldpRemIndex, lldpLocPortTable by lldpLocPortNum.
const (
	OIDLldpRemChassisIDSubtype = "1.0.8802.1.1.2.1.4.1.1.4"
	OIDLldpRemChassisID        = "1.0.8802.1.1.2.1.4.1.1.5"
	OIDLldpRemPortIDSubtype    = "1.0.8802.1.1.2.1.4.1.1.6"
	OIDLldpRemPortID           = "1.0.8802.1.1.2.1.4.1.1.7"
	OIDLldpRemPortDesc         = "1.0.8802.1.1.2.1.4.1.1.8"
	OIDLldpRemSysName          = "1.0.8802.1.1.2.1.4.1.1.9"
	OIDLldpRemSysDesc          = "1.0.8802.1.1.2.1.4.1.1.10"
	// lldpRemManAddrIfSubtype is read only for its index, which carries the
	// neighbour's management address.
	OIDLldpRemManAddrIfSubtype = "1.0.8802.1.1.2.1.4.2.1.3"
	OIDLldpLocPortIDSubtype    = "1.0.8802.1.1.2.1.3.7.1.2"
	OIDLldpLocPortID           = "1.0.8802.1.1.2.1.3.7.1.3"
)

// CISCO-CDP-MIB cdpCacheTable, indexed by cdpCacheIfIndex.cdpCacheDeviceIndex.
// Used when a device reports no LLDP neighbours.
const (
	OIDCdpCacheAddressType           = "1.3.6.1.4.1.9.9.23.1.2.1.1.3"
	OIDCdpCacheAddress               = "1.3.6.1.4.1.9.9.23.1.2.1.1.4"
	OIDCdpCacheVersion               = "1.3.6.1.4.1.9.9.23.1.2.1.1.5"
	OIDCdpCacheDeviceID              = "1.3.6.1.4.1.9.9.23.1.2.1.1.6"
	OIDCdpCacheDevicePort            = "1.3.6.1.4.1.9.9.23.1.2.1.1.7"
	OIDCdpCacheSysName               = "1.3.6.1.4.1.9.9.23.1.2.1.1.17"
	OIDCdpCachePrimaryMgmtAddrType   = "1.3.6.1.4.1.9.9.23.1.2.1.1.19"
	OIDCdpCachePrimaryMgmtAddr       = "1.3.6.1.4.1.9.9.23.1.2.1.1.20"
	OIDCdpCacheSecondaryMgmtAddrType = "1.3.6.1.4.1.9.9.23.1.2.1.1.21"
	OIDCdpCacheSecondaryMgmtAddr     = "1.3.6.1.4.1.9.9.23.1.2.1.1.22"
)

// CISCO-IPSEC-FLOW-MONITOR-MIB cipSecTunnelTable, indexed by cipSecTunIndex.
const (
	OIDCipSecTunLocalAddr  = "1.3.6.1.4.1.9.9.171.1.3.2.1.4"
	OIDCipSecTunRemoteAddr = "1.3.6.1.4.1.9.9.171.1.3.2.1.5"
	OIDCipSecTunLifeSize   = "1.3.6.1.4.1.9.9.171.1.3.2.1.8"
	OIDCipSecTunLifeTime   = "1.3.6.1.4.1.9.9.171.1.3.2.1.9"
	OIDCipSecTunStatus     = "1.3.6.1.4.1.9.9.171.1.3.2.1.51"
)

// Route tables (IP-FORWARD-MIB), old and new, and tunnel tables (TUNNEL-MIB),
// old and new. They give each IPsec tunnel its routed subnets and interface.
const (
	OIDIPCidrRouteIfIndex      = "1.3.6.1.2.1.4.24.4.1.5"
	OIDIPCidrRouteStatus       = "1.3.6.1.2.1.4.24.4.1.16"
	OIDInetCidrRouteIfIndex    = "1.3.6.1.2.1.4.24.7.1.7"
	OIDInetCidrRouteStatus     = "1.3.6.1.2.1.4.24.7.1.17"
	OIDTunnelConfigIfIndex     = "1.3.6.1.2.1.10.131.1.1.2.1.5"
	OIDTunnelInetConfigIfIndex = "1.3.6.1.2.1.10.131.1.1.3.1.6"
)

// inventoryColumns lists the hardcoded columns the options ask for.
func inventoryColumns(opts CompileOptions) []string {
	var out []string
	if opts.IPAddresses {
		out = append(out, OIDIPAdEntIfIndex, OIDIPAdEntNetMask)
	}
	if opts.Topology {
		out = append(out,
			OIDLldpRemChassisIDSubtype, OIDLldpRemChassisID, OIDLldpRemPortIDSubtype,
			OIDLldpRemPortID, OIDLldpRemPortDesc, OIDLldpRemSysName, OIDLldpRemSysDesc,
			OIDLldpRemManAddrIfSubtype, OIDLldpLocPortIDSubtype, OIDLldpLocPortID,
			OIDCdpCacheAddressType, OIDCdpCacheAddress, OIDCdpCacheVersion,
			OIDCdpCacheDeviceID, OIDCdpCacheDevicePort, OIDCdpCacheSysName,
			OIDCdpCachePrimaryMgmtAddrType, OIDCdpCachePrimaryMgmtAddr,
			OIDCdpCacheSecondaryMgmtAddrType, OIDCdpCacheSecondaryMgmtAddr,
		)
	}
	if opts.VPN {
		out = append(out,
			OIDCipSecTunLocalAddr, OIDCipSecTunRemoteAddr, OIDCipSecTunLifeSize,
			OIDCipSecTunLifeTime, OIDCipSecTunStatus,
			OIDIPCidrRouteIfIndex, OIDIPCidrRouteStatus,
			OIDInetCidrRouteIfIndex, OIDInetCidrRouteStatus,
			OIDTunnelConfigIfIndex, OIDTunnelInetConfigIfIndex,
		)
	}
	return out
}
