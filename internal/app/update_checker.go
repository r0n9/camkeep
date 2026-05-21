package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultUpdateCheckURL      = "https://api.github.com/repos/r0n9/camkeep/releases/latest"
	defaultUpdateCheckInterval = 6 * time.Hour
	defaultUpdateCheckTimeout  = 3 * time.Second
)

var updateChecker *UpdateChecker

type UpdateCheckSnapshot struct {
	CurrentVersion      string     `json:"current_version"`
	Channel             string     `json:"channel"`
	LatestStableVersion string     `json:"latest_stable_version,omitempty"`
	UpdateAvailable     bool       `json:"update_available"`
	ReleaseURL          string     `json:"release_url,omitempty"`
	PublishedAt         *time.Time `json:"published_at,omitempty"`
	CheckedAt           *time.Time `json:"checked_at,omitempty"`
	NextCheckAt         *time.Time `json:"next_check_at,omitempty"`
	Checking            bool       `json:"checking"`
	Error               string     `json:"error,omitempty"`
	Message             string     `json:"message,omitempty"`
}

type UpdateChecker struct {
	currentVersion string
	checkURL       string
	interval       time.Duration
	client         *http.Client

	mu       sync.RWMutex
	snapshot UpdateCheckSnapshot
	etag     string
	checking bool
}

type githubLatestRelease struct {
	TagName     string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
}

type parsedVersion struct {
	raw     string
	channel string
	major   int
	minor   int
	patch   int
	stable  bool
}

var stableVersionPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

func NewUpdateChecker(currentVersion string) *UpdateChecker {
	return newUpdateChecker(currentVersion, defaultUpdateCheckURL, defaultUpdateCheckInterval, &http.Client{Timeout: defaultUpdateCheckTimeout})
}

func newUpdateChecker(currentVersion, checkURL string, interval time.Duration, client *http.Client) *UpdateChecker {
	if interval <= 0 {
		interval = defaultUpdateCheckInterval
	}
	if client == nil {
		client = &http.Client{Timeout: defaultUpdateCheckTimeout}
	}

	info := parseVersion(currentVersion)
	return &UpdateChecker{
		currentVersion: strings.TrimSpace(currentVersion),
		checkURL:       checkURL,
		interval:       interval,
		client:         client,
		snapshot: UpdateCheckSnapshot{
			CurrentVersion:  strings.TrimSpace(currentVersion),
			Channel:         info.channel,
			UpdateAvailable: false,
			Message:         versionChannelMessage(info),
		},
	}
}

func (c *UpdateChecker) Start(ctx context.Context) {
	go func() {
		c.Check(ctx)

		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.Check(ctx)
			}
		}
	}()
}

func (c *UpdateChecker) Snapshot() UpdateCheckSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshot
}

func (c *UpdateChecker) Check(parent context.Context) {
	if !c.beginCheck() {
		return
	}

	ctx, cancel := context.WithTimeout(parent, defaultUpdateCheckTimeout)
	defer cancel()

	snapshot, etag, err := c.fetchLatest(ctx)
	c.finishCheck(snapshot, etag, err)
}

func (c *UpdateChecker) beginCheck() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.checking {
		return false
	}
	c.checking = true
	c.snapshot.Checking = true
	c.snapshot.Error = ""
	return true
}

func (c *UpdateChecker) finishCheck(snapshot UpdateCheckSnapshot, etag string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	next := now.Add(c.interval)

	if err != nil {
		c.snapshot.Checking = false
		c.snapshot.CheckedAt = &now
		c.snapshot.NextCheckAt = &next
		c.snapshot.Error = err.Error()
		c.checking = false
		log.Printf("检查 CamKeep 更新失败: %v", err)
		return
	}

	snapshot.Checking = false
	snapshot.CheckedAt = &now
	snapshot.NextCheckAt = &next
	c.snapshot = snapshot
	if etag != "" {
		c.etag = etag
	}
	c.checking = false
}

func (c *UpdateChecker) fetchLatest(ctx context.Context) (UpdateCheckSnapshot, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.checkURL, nil)
	if err != nil {
		return UpdateCheckSnapshot{}, "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "CamKeep/"+safeUserAgentVersion(c.currentVersion))
	if c.etag != "" {
		req.Header.Set("If-None-Match", c.etag)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return UpdateCheckSnapshot{}, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return c.snapshotFromCachedLatest(), resp.Header.Get("ETag"), nil
	}
	if resp.StatusCode != http.StatusOK {
		return UpdateCheckSnapshot{}, "", fmt.Errorf("GitHub release API status=%d", resp.StatusCode)
	}

	var release githubLatestRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return UpdateCheckSnapshot{}, "", err
	}
	if strings.TrimSpace(release.TagName) == "" {
		return UpdateCheckSnapshot{}, "", fmt.Errorf("GitHub release API 未返回 tag_name")
	}

	return c.buildSnapshotFromRelease(release), resp.Header.Get("ETag"), nil
}

func (c *UpdateChecker) snapshotFromCachedLatest() UpdateCheckSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	snapshot := c.snapshot
	snapshot.Error = ""
	return snapshot
}

func (c *UpdateChecker) buildSnapshotFromRelease(release githubLatestRelease) UpdateCheckSnapshot {
	current := parseVersion(c.currentVersion)
	latest := parseVersion(release.TagName)
	message := versionChannelMessage(current)
	updateAvailable := false

	if current.stable && latest.stable {
		updateAvailable = compareStableVersion(latest, current) > 0
		if updateAvailable {
			message = fmt.Sprintf("发现新版本 %s", release.TagName)
		} else {
			message = "当前已是最新稳定版"
		}
	} else if current.stable && !latest.stable {
		message = "最新 Release 版本号不可识别，暂不判断更新"
	}

	return UpdateCheckSnapshot{
		CurrentVersion:      c.currentVersion,
		Channel:             current.channel,
		LatestStableVersion: strings.TrimSpace(release.TagName),
		UpdateAvailable:     updateAvailable,
		ReleaseURL:          strings.TrimSpace(release.HTMLURL),
		PublishedAt:         normalizeOptionalTime(release.PublishedAt),
		Message:             message,
	}
}

func parseVersion(version string) parsedVersion {
	raw := strings.TrimSpace(version)
	lower := strings.ToLower(raw)
	info := parsedVersion{raw: raw, channel: "custom"}

	switch {
	case lower == "" || lower == "dev" || strings.HasPrefix(lower, "dev-") || strings.Contains(lower, "-dev"):
		info.channel = "dev"
		return info
	case lower == "test" || strings.HasPrefix(lower, "test-") || strings.Contains(lower, "-test"):
		info.channel = "test"
		return info
	}

	matches := stableVersionPattern.FindStringSubmatch(raw)
	if len(matches) != 4 {
		return info
	}

	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])
	info.channel = "stable"
	info.major = major
	info.minor = minor
	info.patch = patch
	info.stable = true
	return info
}

func compareStableVersion(a, b parsedVersion) int {
	if a.major != b.major {
		return compareInt(a.major, b.major)
	}
	if a.minor != b.minor {
		return compareInt(a.minor, b.minor)
	}
	return compareInt(a.patch, b.patch)
}

func compareInt(a, b int) int {
	if a > b {
		return 1
	}
	if a < b {
		return -1
	}
	return 0
}

func versionChannelMessage(info parsedVersion) string {
	switch info.channel {
	case "stable":
		return "等待检查最新稳定版"
	case "dev":
		return "当前为开发版本，不参与稳定版更新判断"
	case "test":
		return "当前为测试版本，不参与稳定版更新判断"
	default:
		return "当前版本号不可识别，不参与稳定版更新判断"
	}
}

func normalizeOptionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func safeUserAgentVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return strings.Map(func(r rune) rune {
		if r <= 32 || r >= 127 {
			return '-'
		}
		return r
	}, value)
}
