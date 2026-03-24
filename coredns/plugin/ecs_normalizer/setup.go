package ecs_normalizer

import (
	"fmt"
	"strconv"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/dgraph-io/ristretto"
	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
)

var log = clog.NewWithPlugin("ecs_normalizer")

func init() {
	plugin.Register("ecs_normalizer", setup)
}

// Config holds Corefile options for the ecs_normalizer plugin.
type Config struct {
	IP2RegionXDB   string
	NacosAddr      string
	NacosNamespace string
	NacosGroup     string
	NacosDataID    string
	PrefixLength   uint8
}

func setup(c *caddy.Controller) error {
	cfg, err := parseConfig(c)
	if err != nil {
		return plugin.Error("ecs_normalizer", err)
	}

	// Load ip2region XDB entirely into memory for lock-free concurrent reads.
	cBuff, err := xdb.LoadContentFromFile(cfg.IP2RegionXDB)
	if err != nil {
		return plugin.Error("ecs_normalizer", fmt.Errorf("load ip2region xdb: %w", err))
	}
	searcher, err := xdb.NewWithBuffer(xdb.IPv4, cBuff)
	if err != nil {
		return plugin.Error("ecs_normalizer", fmt.Errorf("init searcher: %w", err))
	}

	// Ristretto in-memory DNS response cache (province|isp|qname|qtype → response).
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e5,        // track frequency of 100k keys
		MaxCost:     100 << 20,  // max 100 MB
		BufferItems: 64,
	})
	if err != nil {
		return plugin.Error("ecs_normalizer", fmt.Errorf("init ristretto cache: %w", err))
	}

	e := &ECSNormalizer{
		cfg:      cfg,
		searcher: searcher,
		dnsCache: cache,
	}

	nacosClient, err := newNacosClient(cfg)
	if err != nil {
		return plugin.Error("ecs_normalizer", fmt.Errorf("init nacos client: %w", err))
	}
	if err := e.loadSubnetMap(nacosClient); err != nil {
		return plugin.Error("ecs_normalizer", fmt.Errorf("load subnet map from nacos: %w", err))
	}
	e.startNacosListener(nacosClient)

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		e.Next = next
		return e
	})
	return nil
}

func parseConfig(c *caddy.Controller) (*Config, error) {
	cfg := &Config{
		NacosGroup:   "subnet_mapping",
		NacosDataID:  "subnet_map",
		PrefixLength: 24,
	}
	for c.Next() {
		for c.NextBlock() {
			switch c.Val() {
			case "ip2region_db":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				cfg.IP2RegionXDB = c.Val()
			case "nacos_addr":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				cfg.NacosAddr = c.Val()
			case "nacos_namespace":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				cfg.NacosNamespace = c.Val()
			case "nacos_group":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				cfg.NacosGroup = c.Val()
			case "nacos_data_id":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				cfg.NacosDataID = c.Val()
			case "prefix_length":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				n, err := strconv.ParseUint(c.Val(), 10, 8)
				if err != nil {
					return nil, fmt.Errorf("invalid prefix_length %q: %w", c.Val(), err)
				}
				cfg.PrefixLength = uint8(n)
			default:
				return nil, c.Errf("unknown option %q", c.Val())
			}
		}
	}
	if cfg.IP2RegionXDB == "" {
		return nil, fmt.Errorf("ip2region_db is required")
	}
	if cfg.NacosAddr == "" {
		return nil, fmt.Errorf("nacos_addr is required")
	}
	return cfg, nil
}
