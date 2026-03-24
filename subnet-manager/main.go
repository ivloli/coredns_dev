package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"

	"coredns-dev/subnet-manager/config"
	"coredns-dev/subnet-manager/watcher"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// Ensure ip2region data directory exists.
	if err := os.MkdirAll(cfg.IP2Region.Dir, 0755); err != nil {
		log.Fatalf("create data dir %s: %v", cfg.IP2Region.Dir, err)
	}

	host, port := splitHostPort(cfg.Nacos.Addr)
	sc := []constant.ServerConfig{
		*constant.NewServerConfig(host, port),
	}
	cc := *constant.NewClientConfig(
		constant.WithNamespaceId(cfg.Nacos.Namespace),
		constant.WithTimeoutMs(5000),
		constant.WithNotLoadCacheAtStart(true),
		constant.WithLogDir("/tmp/nacos/log"),
		constant.WithCacheDir("/tmp/nacos/cache"),
		constant.WithLogLevel("warn"),
	)
	nacosClient, err := clients.NewConfigClient(vo.NacosClientParam{
		ClientConfig:  &cc,
		ServerConfigs: sc,
	})
	if err != nil {
		log.Fatalf("init nacos client: %v", err)
	}

	w := &watcher.VersionWatcher{
		TXTPath:      cfg.IP2Region.TXTPath,
		XDBPath:      cfg.IP2Region.XDBPath,
		VersionFile:  filepath.Join(cfg.IP2Region.Dir, ".version"),
		PollInterval: cfg.IP2Region.PollInterval,
		GithubToken:  cfg.IP2Region.GithubToken,
		NacosClient:  nacosClient,
		NacosGroup:   cfg.Nacos.Group,
		NacosDataID:  cfg.Nacos.DataID,
	}

	log.Printf("subnet-manager starting (poll_interval=%s, nacos=%s)",
		cfg.IP2Region.PollInterval, cfg.Nacos.Addr)
	w.Start() // blocks forever
}

// splitHostPort splits "host:port", returning port=8848 on any parse error.
func splitHostPort(addr string) (host string, port uint64) {
	port = 8848
	idx := strings.LastIndex(addr, ":")
	if idx < 0 {
		return addr, port
	}
	host = addr[:idx]
	if p, err := strconv.ParseUint(addr[idx+1:], 10, 64); err == nil {
		port = p
	}
	return
}
