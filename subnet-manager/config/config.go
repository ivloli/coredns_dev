package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Nacos     NacosConfig     `yaml:"nacos"`
	IP2Region IP2RegionConfig `yaml:"ip2region"`
	API       APIConfig       `yaml:"api"`
}

type NacosConfig struct {
	Addr      string `yaml:"addr"`
	Namespace string `yaml:"namespace"`
	Group     string `yaml:"group"`
	DataID    string `yaml:"data_id"`
	Username  string `yaml:"username"`
	Password  string `yaml:"password"`
}

type IP2RegionConfig struct {
	Dir          string        `yaml:"dir"`
	TXTPath      string        `yaml:"txt_path"`
	XDBPath      string        `yaml:"xdb_path"`
	PollInterval time.Duration `yaml:"poll_interval"`
	GithubToken  string        `yaml:"github_token"`
	ReleasesURL  string        `yaml:"releases_url"`
	TXTDownURL   string        `yaml:"txt_download_url"`
	XDBDownURL   string        `yaml:"xdb_download_url"`
}

type APIConfig struct {
	Listen string `yaml:"listen"`
	Token  string `yaml:"token"`
}

// Load reads a YAML config file and expands environment variables.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	// Expand ${VAR} / $VAR before unmarshalling.
	expanded := os.ExpandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, cfg.validate()
}

func (c *Config) validate() error {
	if c.Nacos.Addr == "" {
		return fmt.Errorf("nacos.addr is required")
	}
	if c.Nacos.Group == "" {
		c.Nacos.Group = "subnet_mapping"
	}
	if c.Nacos.DataID == "" {
		c.Nacos.DataID = "subnet_map"
	}
	if c.IP2Region.TXTPath == "" {
		return fmt.Errorf("ip2region.txt_path is required")
	}
	if c.IP2Region.XDBPath == "" {
		return fmt.Errorf("ip2region.xdb_path is required")
	}
	if c.IP2Region.PollInterval <= 0 {
		c.IP2Region.PollInterval = time.Hour
	}
	if c.IP2Region.ReleasesURL == "" {
		c.IP2Region.ReleasesURL = "https://api.github.com/repos/lionsoul2014/ip2region/releases/latest"
	}
	if c.IP2Region.TXTDownURL == "" {
		c.IP2Region.TXTDownURL = "https://raw.githubusercontent.com/lionsoul2014/ip2region/master/data/ipv4_source.txt"
	}
	if c.IP2Region.XDBDownURL == "" {
		c.IP2Region.XDBDownURL = "https://raw.githubusercontent.com/lionsoul2014/ip2region/master/data/ip2region_v4.xdb"
	}
	return nil
}
