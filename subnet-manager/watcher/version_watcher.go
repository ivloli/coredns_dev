package watcher

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"

	"coredns-dev/subnet-manager/syncer"
)

const (
	githubReleasesURL = "https://api.github.com/repos/lionsoul2014/ip2region/releases/latest"
	txtDownloadURL    = "https://raw.githubusercontent.com/lionsoul2014/ip2region/main/data/ipv4_source.txt"
	xdbDownloadURL    = "https://raw.githubusercontent.com/lionsoul2014/ip2region/main/data/ip2region_v4.xdb"
)

// VersionWatcher polls the ip2region GitHub release tag.
// When the tag changes it downloads the new data files and syncs Nacos.
type VersionWatcher struct {
	TXTPath      string
	XDBPath      string
	VersionFile  string        // persisted local version tag, e.g. /data/ip2region/.version
	PollInterval time.Duration
	GithubToken  string // optional; prevents hitting the 60 req/h anonymous limit
	NacosClient  config_client.IConfigClient
	NacosGroup   string
	NacosDataID  string
}

type githubRelease struct {
	TagName string `json:"tag_name"`
}

// Start checks once on startup, then polls on PollInterval. Blocks forever.
func (w *VersionWatcher) Start() {
	if err := w.checkAndUpdate(); err != nil {
		log.Printf("[watcher] startup check failed: %v", err)
	}
	ticker := time.NewTicker(w.PollInterval)
	defer ticker.Stop()
	for range ticker.C {
		if err := w.checkAndUpdate(); err != nil {
			log.Printf("[watcher] check failed: %v", err)
		}
	}
}

func (w *VersionWatcher) checkAndUpdate() error {
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // 跳过证书验证，适配有 TLS 拦截的网络环境
		},
	}

	// 1. Fetch latest GitHub release tag.
	latestTag, err := w.fetchLatestTag(httpClient)
	if err != nil {
		return fmt.Errorf("fetch latest tag: %w", err)
	}

	// 2. Compare with locally persisted version.
	localTag := w.readLocalVersion()
	if localTag == latestTag {
		log.Printf("[watcher] already at latest (%s), nothing to do", latestTag)
		return nil
	}
	log.Printf("[watcher] version %q → %q, downloading...", localTag, latestTag)

	// 3. Download new files (atomic tmp → rename).
	if err := downloadFile(httpClient, txtDownloadURL, w.TXTPath); err != nil {
		return fmt.Errorf("download ipv4_source.txt: %w", err)
	}
	if err := downloadFile(httpClient, xdbDownloadURL, w.XDBPath); err != nil {
		return fmt.Errorf("download ip2region_v4.xdb: %w", err)
	}
	log.Printf("[watcher] files downloaded")

	// 4. Sync updated data to Nacos.
	s := &syncer.Syncer{
		NacosClient: w.NacosClient,
		NacosGroup:  w.NacosGroup,
		NacosDataID: w.NacosDataID,
		TXTPath:     w.TXTPath,
		XDBPath:     w.XDBPath,
	}
	if err := s.Sync(); err != nil {
		return fmt.Errorf("sync nacos: %w", err)
	}

	// 5. Persist version only after full success (guarantees idempotency on retry).
	if err := w.writeLocalVersion(latestTag); err != nil {
		log.Printf("[watcher] warning: write local version failed: %v", err)
	}
	log.Printf("[watcher] update complete, current version: %s", latestTag)
	return nil
}

func (w *VersionWatcher) fetchLatestTag(hc *http.Client) (string, error) {
	req, err := http.NewRequest("GET", githubReleasesURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if w.GithubToken != "" {
		req.Header.Set("Authorization", "Bearer "+w.GithubToken)
	}

	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github API returned %d", resp.StatusCode)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	return strings.TrimSpace(rel.TagName), nil
}

func (w *VersionWatcher) readLocalVersion() string {
	data, _ := os.ReadFile(w.VersionFile)
	return strings.TrimSpace(string(data))
}

func (w *VersionWatcher) writeLocalVersion(tag string) error {
	return os.WriteFile(w.VersionFile, []byte(tag+"\n"), 0644)
}

// downloadFile downloads url to destPath via a temporary file (atomic rename).
// Logs the SHA-256 of the downloaded content for auditability.
func downloadFile(hc *http.Client, url, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}
	resp, err := hc.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}

	tmp := destPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err = io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	log.Printf("[watcher] %s sha256:%x", filepath.Base(destPath), h.Sum(nil))
	return os.Rename(tmp, destPath)
}
