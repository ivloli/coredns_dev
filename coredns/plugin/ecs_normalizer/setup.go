package ecs_normalizer

import (
	"fmt"
	"strconv"
	"strings"
	"time"

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
	IP2RegionXDB       string
	NacosAddr          string
	NacosNamespace     string
	NacosGroup         string
	NacosDataID        string
	NacosUsername      string
	NacosPassword      string
	PrefixLength       uint8
	CachePrefetchAhead time.Duration
	CachePrefetchMode  string
	CachePrefetchScan  time.Duration
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
	log.Infof("ip2region xdb loaded: %s (%d bytes)", cfg.IP2RegionXDB, len(cBuff))

	// Ristretto in-memory DNS response cache (province|isp|qname|qtype → response).
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e5,       // track frequency of 100k keys
		MaxCost:     100 << 20, // max 100 MB
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
	log.Infof("ecs_normalizer ready: nacos=%s ns=%q group=%s data_id=%s prefix_len=/%d prefetch_mode=%s prefetch_ahead=%s prefetch_scan=%s",
		cfg.NacosAddr, cfg.NacosNamespace, cfg.NacosGroup, cfg.NacosDataID, cfg.PrefixLength, cfg.CachePrefetchMode, cfg.CachePrefetchAhead, cfg.CachePrefetchScan)

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		e.Next = next
		e.startActivePrefetchLoop()
		return e
	})
	return nil
}

func parseConfig(c *caddy.Controller) (*Config, error) {
	cfg := &Config{
		NacosGroup:         "subnet_mapping",
		NacosDataID:        "subnet_map",
		PrefixLength:       24,
		CachePrefetchAhead: 5 * time.Second,
		CachePrefetchMode:  "request",
		CachePrefetchScan:  1 * time.Second,
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
			case "nacos_username":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				cfg.NacosUsername = c.Val()
			case "nacos_password":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				cfg.NacosPassword = c.Val()
			case "prefix_length":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				n, err := strconv.ParseUint(c.Val(), 10, 8)
				if err != nil {
					return nil, fmt.Errorf("invalid prefix_length %q: %w", c.Val(), err)
				}
				cfg.PrefixLength = uint8(n)
			case "cache_prefetch_ahead":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				d, err := time.ParseDuration(c.Val())
				if err != nil {
					return nil, fmt.Errorf("invalid cache_prefetch_ahead %q: %w", c.Val(), err)
				}
				if d < 0 {
					return nil, fmt.Errorf("cache_prefetch_ahead must be >= 0")
				}
				cfg.CachePrefetchAhead = d
			case "cache_prefetch_mode":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				mode := strings.ToLower(c.Val())
				if mode != "request" && mode != "active" {
					return nil, fmt.Errorf("invalid cache_prefetch_mode %q: must be request|active", c.Val())
				}
				cfg.CachePrefetchMode = mode
			case "cache_prefetch_scan":
				if !c.NextArg() {
					return nil, c.ArgErr()
				}
				d, err := time.ParseDuration(c.Val())
				if err != nil {
					return nil, fmt.Errorf("invalid cache_prefetch_scan %q: %w", c.Val(), err)
				}
				if d <= 0 {
					return nil, fmt.Errorf("cache_prefetch_scan must be > 0")
				}
				cfg.CachePrefetchScan = d
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
