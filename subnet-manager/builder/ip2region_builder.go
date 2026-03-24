package builder

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

// SubnetEntry holds the representative CIDR for a province+ISP pair.
type SubnetEntry struct {
	Province string
	ISP      string
	CIDR     string
}

// BuildSubnetMap iterates the ip2region ipv4_source.txt file and returns a
// map of "province|isp" → representative CIDR subnet.
//
// File format (one record per line):
//
//	startIP|endIP|country|province|city|ISP|regionCode
//	Example: 1.0.8.0|1.0.15.255|中国|广东省|广州市|中国电信|CN
//
// Only mainland China records are included (country == "中国").
// Province and ISP names are normalized (see normalizeProvince / normalizeISP).
// The CIDR is computed from the actual IP range, not a fixed /24.
// The first segment encountered for each (province, ISP) pair is used.
func BuildSubnetMap(txtPath string) (map[string]string, error) {
	f, err := os.Open(txtPath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", txtPath, err)
	}
	defer f.Close()

	firstIPMap := make(map[string][2]uint32) // key → [startIP, endIP]
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	lineCount, validCount := 0, 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lineCount++
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// startIP|endIP|country|province|city|ISP|regionCode
		parts := strings.Split(line, "|")
		if len(parts) < 7 {
			continue
		}

		country := strings.TrimSpace(parts[2])
		if country != "中国" {
			continue
		}
		validCount++

		province := strings.TrimSpace(parts[3])
		isp := strings.TrimSpace(parts[5])
		if province == "" || province == "0" || isp == "" || isp == "0" {
			continue
		}

		province = NormalizeProvince(province)
		isp = NormalizeISP(isp)

		startIP, err := ipToUint32(parts[0])
		if err != nil {
			continue
		}
		endIP, err := ipToUint32(parts[1])
		if err != nil {
			continue
		}

		key := province + "|" + isp
		if _, exists := firstIPMap[key]; !exists {
			firstIPMap[key] = [2]uint32{startIP, endIP}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", txtPath, err)
	}

	log.Printf("[builder] scanned %d lines, %d china records, %d province|isp keys",
		lineCount, validCount, len(firstIPMap))

	result := make(map[string]string, len(firstIPMap))
	for key, r := range firstIPMap {
		result[key] = toCIDR(r[0], r[1])
	}
	return result, nil
}

// NormalizeProvince strips trailing "省" / "市" so keys are consistent with
// what ip2region's SearchByStr also returns after normalization.
// "广东省" → "广东", "北京市" → "北京"
func NormalizeProvince(province string) string {
	province = strings.TrimSuffix(province, "省")
	province = strings.TrimSuffix(province, "市")
	return strings.TrimSpace(province)
}

// NormalizeISP strips "中国" and "云" so keys stay short and stable.
// "中国电信" → "电信", "中国联通" → "联通", "中国移动" → "移动"
func NormalizeISP(isp string) string {
	isp = strings.ReplaceAll(isp, "中国", "")
	isp = strings.ReplaceAll(isp, "云", "")
	return strings.TrimSpace(isp)
}

// toCIDR computes a CIDR block from a start/end IP pair.
// The prefix length is chosen as the smallest power-of-2 block that covers
// the range, which avoids always hard-coding /24.
// e.g. a 256-address range → /24; a 512-address range → /23.
func toCIDR(startIP, endIP uint32) string {
	count := endIP - startIP + 1
	var prefixLen int
	for i := 0; i < 32; i++ {
		if (1 << i) >= int(count) {
			prefixLen = 32 - i
			break
		}
	}
	return fmt.Sprintf("%s/%d", uint32ToIP(startIP), prefixLen)
}

func ipToUint32(ip string) (uint32, error) {
	parts := strings.Split(strings.TrimSpace(ip), ".")
	if len(parts) != 4 {
		return 0, fmt.Errorf("invalid IP: %s", ip)
	}
	var r uint32
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return 0, fmt.Errorf("invalid IP octet: %s", p)
		}
		r = r<<8 | uint32(n)
	}
	return r, nil
}

func uint32ToIP(n uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d", n>>24, (n>>16)&0xff, (n>>8)&0xff, n&0xff)
}
