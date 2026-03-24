package syncer

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"

	"coredns-dev/subnet-manager/builder"
)
// Syncer compares the ip2region TXT file against the current Nacos subnet_map
// config and pushes a corrected, merged result back to Nacos.
type Syncer struct {
	NacosClient config_client.IConfigClient
	NacosGroup  string
	NacosDataID string
	TXTPath     string
	XDBPath     string
}

// Sync runs one full diff-and-update cycle:
//  1. Build ip2regionMap from TXT file.
//  2. Load nacosMap from Nacos.
//  3. For each key in nacosMap: probe its subnet via XDB; delete if stale/wrong.
//  4. Add keys present in ip2regionMap but missing from nacosMap.
//  5. Publish merged map to Nacos (only when there are actual changes).
func (s *Syncer) Sync() error {
	// Step 1 — build from TXT
	ip2regionMap, err := builder.BuildSubnetMap(s.TXTPath)
	if err != nil {
		return fmt.Errorf("build subnet map: %w", err)
	}
	log.Printf("[syncer] ip2region TXT: %d entries", len(ip2regionMap))

	// Step 2 — load from Nacos
	nacosMap, err := s.loadNacosMap()
	if err != nil {
		return fmt.Errorf("load nacos map: %w", err)
	}
	log.Printf("[syncer] nacos current: %d entries", len(nacosMap))

	// Step 3 — load XDB for reverse validation
	cBuff, err := xdb.LoadContentFromFile(s.XDBPath)
	if err != nil {
		return fmt.Errorf("load xdb: %w", err)
	}
	searcher, err := xdb.NewWithBuffer(xdb.IPv4, cBuff)
	if err != nil {
		return fmt.Errorf("init xdb searcher: %w", err)
	}
	defer searcher.Close()

	// Step 4 — remove stale / incorrect keys
	removed := 0
	for key, subnet := range nacosMap {
		probeIP, err := subnetProbeIP(subnet)
		if err != nil {
			log.Printf("[syncer] delete %q: invalid subnet %q", key, subnet)
			delete(nacosMap, key)
			removed++
			continue
		}
		regionStr, err := searcher.SearchByStr(probeIP)
		if err != nil || regionStr == "" {
			log.Printf("[syncer] delete %q: ip2region lookup failed", key)
			delete(nacosMap, key)
			removed++
			continue
		}
		currentKey := regionToKey(regionStr)
		if currentKey != key {
			log.Printf("[syncer] delete %q: region changed to %q", key, currentKey)
			delete(nacosMap, key)
			removed++
		}
	}
	log.Printf("[syncer] removed %d stale entries", removed)

	// Step 5 — add missing keys
	added := 0
	for key, subnet := range ip2regionMap {
		if _, exists := nacosMap[key]; !exists {
			nacosMap[key] = subnet
			added++
		}
	}
	log.Printf("[syncer] added %d new entries", added)

	if removed == 0 && added == 0 {
		log.Printf("[syncer] no changes detected, skip publish")
		return nil
	}

	// Step 6 — publish merged map
	data, err := json.Marshal(nacosMap)
	if err != nil {
		return fmt.Errorf("marshal nacos map: %w", err)
	}
	ok, err := s.NacosClient.PublishConfig(vo.ConfigParam{
		DataId:  s.NacosDataID,
		Group:   s.NacosGroup,
		Content: string(data),
	})
	if err != nil {
		return fmt.Errorf("publish nacos config: %w", err)
	}
	if !ok {
		return fmt.Errorf("publish nacos config returned false")
	}
	log.Printf("[syncer] published to nacos: %d total entries (+%d -%d)", len(nacosMap), added, removed)
	return nil
}

func (s *Syncer) loadNacosMap() (map[string]string, error) {
	content, err := s.NacosClient.GetConfig(vo.ConfigParam{
		DataId: s.NacosDataID,
		Group:  s.NacosGroup,
	})
	if err != nil {
		return nil, err
	}
	m := make(map[string]string)
	if content != "" {
		if err := json.Unmarshal([]byte(content), &m); err != nil {
			return nil, fmt.Errorf("unmarshal nacos config: %w", err)
		}
	}
	return m, nil
}

// subnetProbeIP returns a usable probe IP from a CIDR (network address + 1).
// E.g. "101.32.5.0/24" → "101.32.5.1"
func subnetProbeIP(cidr string) (string, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", err
	}
	probe := make(net.IP, len(ipNet.IP))
	copy(probe, ipNet.IP)
	probe[len(probe)-1]++
	return probe.String(), nil
}

// regionToKey converts an ip2region SearchByStr result to a Nacos map key.
// SearchByStr format: "中国|0|广东省|广州市|电信"
// Output key uses the same normalization as the builder: "广东|电信"
func regionToKey(regionStr string) string {
	parts := strings.Split(regionStr, "|")
	if len(parts) < 5 {
		return ""
	}
	province := builder.NormalizeProvince(strings.TrimSpace(parts[2]))
	isp := builder.NormalizeISP(strings.TrimSpace(parts[4]))
	if province == "" || province == "0" || isp == "" || isp == "0" {
		return ""
	}
	return province + "|" + isp
}
