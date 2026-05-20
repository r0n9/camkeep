package app

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/r0n9/camkeep/constant"
	"github.com/r0n9/camkeep/internal/service"
)

const (
	cameraCoverWidth                  = 320
	cameraCoverHeight                 = 180
	cameraCoverMaxBytes               = 1 << 20
	cameraCoverRefreshActiveTTL       = 20 * time.Second
	cameraCoverRefreshIdleTTL         = 3 * time.Minute
	cameraCoverRefreshDefaultTTL      = 45 * time.Second
	cameraCoverRetryBackoff           = 45 * time.Second
	cameraCoverRetryIdleBackoff       = 2 * time.Minute
	cameraCoverFetchTimeout           = 4 * time.Second
	cameraCoverMaxConcurrentRefreshes = 2
	cameraCoverFileName               = "cover.jpg"
	cameraCoverTaskInterval           = 10 * time.Minute
)

type cameraCoverFFmpegRunner func(ctx context.Context, args ...string) ([]byte, error)

type cameraCoverSnapshot struct {
	Ready     bool
	Loading   bool
	Version   int64
	UpdatedAt time.Time
}

type cameraCoverEntry struct {
	Content     []byte
	ContentType string
	UpdatedAt   time.Time
	Version     int64
	DiskLoaded  bool
	InFlight    bool
	NextAttempt time.Time
	LastError   string
	LastErrorAt time.Time
	LastFetchAt time.Time
}

type coverRefreshStatus struct {
	StreamState string
	IsRunning   bool
}

type cameraCoverStore struct {
	mu        sync.RWMutex
	entries   map[string]*cameraCoverEntry
	client    *http.Client
	baseURL   string
	recordDir string
	ffmpegRun cameraCoverFFmpegRunner
	enabled   bool
	inFlight  int
}

var cameraCovers = newCameraCoverStore()

func newCameraCoverStore() *cameraCoverStore {
	return &cameraCoverStore{
		entries: make(map[string]*cameraCoverEntry),
		client: &http.Client{
			Timeout: cameraCoverFetchTimeout,
		},
		ffmpegRun: runCameraCoverFFmpeg,
		enabled:   true,
	}
}

func (s *cameraCoverStore) touch(camID, streamState string, isRunning bool) {
	if !s.enabled || strings.TrimSpace(camID) == "" {
		return
	}

	now := time.Now()

	s.mu.Lock()
	entry := s.ensureEntryLocked(camID)
	if !s.shouldRefreshLocked(entry, now, streamState, isRunning) {
		s.mu.Unlock()
		return
	}
	if s.inFlight >= cameraCoverMaxConcurrentRefreshes {
		s.mu.Unlock()
		return
	}

	entry.InFlight = true
	entry.LastFetchAt = now
	s.inFlight++
	s.mu.Unlock()

	go s.refresh(camID, streamState, isRunning)
}

func (s *cameraCoverStore) touchAll(statuses map[string]coverRefreshStatus) {
	for camID, status := range statuses {
		s.touch(camID, status.StreamState, status.IsRunning)
	}
}

func CameraCoverTask(ctx context.Context) {
	ticker := time.NewTicker(cameraCoverTaskInterval)
	defer ticker.Stop()

	cameraCovers.touchAll(snapshotCoverRefreshStatuses())

	for {
		select {
		case <-ctx.Done():
			log.Println("摄像头封面刷新任务已停止")
			return
		case <-ticker.C:
			cameraCovers.touchAll(snapshotCoverRefreshStatuses())
		}
	}
}

func snapshotCoverRefreshStatuses() map[string]coverRefreshStatus {
	service.StatusMux.RLock()
	defer service.StatusMux.RUnlock()

	statuses := make(map[string]coverRefreshStatus, len(service.StatusMap))
	for id, status := range service.StatusMap {
		statuses[id] = coverRefreshStatus{
			StreamState: status.StreamState,
			IsRunning:   status.IsRunning,
		}
	}
	return statuses
}

func (s *cameraCoverStore) snapshot(camID string) cameraCoverSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := s.ensureEntryLocked(camID)
	return cameraCoverSnapshot{
		Ready:     len(entry.Content) > 0,
		Loading:   entry.InFlight,
		Version:   entry.Version,
		UpdatedAt: entry.UpdatedAt,
	}
}

func (s *cameraCoverStore) image(camID string) ([]byte, string, bool) {
	s.mu.Lock()
	entry := s.ensureEntryLocked(camID)
	if len(entry.Content) == 0 {
		s.mu.Unlock()
		return nil, "", false
	}
	content := append([]byte(nil), entry.Content...)
	contentType := entry.ContentType
	s.mu.Unlock()

	if contentType == "" {
		contentType = "image/jpeg"
	}
	return content, contentType, true
}

func (s *cameraCoverStore) prune(validIDs map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for camID := range s.entries {
		if !validIDs[camID] {
			delete(s.entries, camID)
		}
	}
}

func (s *cameraCoverStore) ensureEntryLocked(camID string) *cameraCoverEntry {
	if entry, ok := s.entries[camID]; ok {
		return entry
	}

	entry := &cameraCoverEntry{}
	s.entries[camID] = entry
	s.loadFromDiskLocked(camID, entry)
	return entry
}

func (s *cameraCoverStore) shouldRefreshLocked(entry *cameraCoverEntry, now time.Time, streamState string, isRunning bool) bool {
	if streamState == "offline" || entry.InFlight {
		return false
	}

	hasCover := len(entry.Content) > 0
	if !hasCover {
		return true
	}
	if now.Before(entry.NextAttempt) {
		return false
	}
	ttl := cameraCoverRefreshTTL(streamState, isRunning)
	if !entry.UpdatedAt.IsZero() && now.Sub(entry.UpdatedAt) < ttl {
		return false
	}

	return true
}

func (s *cameraCoverStore) refresh(camID, streamState string, isRunning bool) {
	content, contentType, err := s.fetch(camID)
	var saveErr error
	if err == nil {
		saveErr = s.saveToDisk(camID, content)
	}
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	entry := s.ensureEntryLocked(camID)
	entry.InFlight = false
	if s.inFlight > 0 {
		s.inFlight--
	}

	if err != nil {
		entry.LastError = err.Error()
		entry.LastErrorAt = now
		entry.NextAttempt = now.Add(cameraCoverRetryDelay(streamState))
		return
	}
	if saveErr != nil {
		entry.LastError = saveErr.Error()
		entry.LastErrorAt = now
		entry.NextAttempt = now.Add(cameraCoverRetryDelay(streamState))
		return
	}

	entry.Content = append([]byte(nil), content...)
	entry.ContentType = contentType
	entry.UpdatedAt = now
	entry.Version = now.UnixMilli()
	entry.NextAttempt = time.Time{}
	entry.LastError = ""
}

func (s *cameraCoverStore) fetch(camID string) ([]byte, string, error) {
	content, contentType, err := s.fetchGo2rtcFrame(camID)
	if err == nil {
		return content, contentType, nil
	}

	ffmpegContent, ffmpegErr := s.fetchFFmpegFrame(camID)
	if ffmpegErr == nil {
		log.Printf("[%s] go2rtc 封面获取失败，已使用 ffmpeg 兜底: %v", camID, err)
		return ffmpegContent, "image/jpeg", nil
	}
	return nil, "", fmt.Errorf("go2rtc snapshot failed: %w; ffmpeg fallback failed: %v", err, ffmpegErr)
}

func (s *cameraCoverStore) fetchGo2rtcFrame(camID string) ([]byte, string, error) {
	req, err := http.NewRequest(http.MethodGet, s.snapshotURL(camID), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "image/jpeg")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("snapshot status=%d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, cameraCoverMaxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(body) == 0 {
		return nil, "", fmt.Errorf("empty snapshot body")
	}
	if len(body) > cameraCoverMaxBytes {
		return nil, "", fmt.Errorf("snapshot too large")
	}

	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "image/jpeg"
	}
	return body, contentType, nil
}

func (s *cameraCoverStore) fetchFFmpegFrame(camID string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cameraCoverFetchTimeout)
	defer cancel()

	runner := s.ffmpegRun
	if runner == nil {
		runner = runCameraCoverFFmpeg
	}

	body, err := runner(ctx,
		"-loglevel", "error",
		"-rtsp_transport", "tcp",
		"-timeout", "5000000",
		"-i", cameraCoverRTSPURL(camID),
		"-an",
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease", cameraCoverWidth, cameraCoverHeight),
		"-q:v", "4",
		"-f", "image2",
		"-vcodec", "mjpeg",
		"-",
	)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("empty ffmpeg snapshot body")
	}
	if len(body) > cameraCoverMaxBytes {
		return nil, fmt.Errorf("ffmpeg snapshot too large")
	}
	return body, nil
}

func runCameraCoverFFmpeg(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return output, nil
}

func (s *cameraCoverStore) snapshotURL(camID string) string {
	baseURL := s.baseURL
	if strings.TrimSpace(baseURL) == "" {
		baseURL = fmt.Sprintf("http://%s:%d", constant.DefaultGo2rtcHost, constant.DefaultGo2rtcApiPort)
	}

	query := url.Values{}
	query.Set("src", camID)
	query.Set("width", strconv.Itoa(cameraCoverWidth))
	query.Set("height", strconv.Itoa(cameraCoverHeight))

	return baseURL + "/api/frame.jpeg?" + query.Encode()
}

func cameraCoverRTSPURL(camID string) string {
	return fmt.Sprintf("rtsp://%s:8554/%s", constant.DefaultGo2rtcHost, camID)
}

func (s *cameraCoverStore) coverPath(camID string) string {
	baseDir := s.recordDir
	if strings.TrimSpace(baseDir) == "" {
		baseDir = constant.DefaultRecordBaseDir
	}
	return filepath.Join(baseDir, camID, cameraCoverFileName)
}

func (s *cameraCoverStore) loadFromDiskLocked(camID string, entry *cameraCoverEntry) {
	if entry.DiskLoaded {
		return
	}
	entry.DiskLoaded = true

	path := s.coverPath(camID)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return
	}

	content, err := os.ReadFile(path)
	if err != nil || len(content) == 0 || len(content) > cameraCoverMaxBytes {
		return
	}

	entry.Content = content
	entry.ContentType = "image/jpeg"
	entry.UpdatedAt = info.ModTime()
	entry.Version = info.ModTime().UnixMilli()
}

func (s *cameraCoverStore) saveToDisk(camID string, content []byte) error {
	path := s.coverPath(camID)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create cover dir: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, content, 0644); err != nil {
		return fmt.Errorf("write cover: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace cover: %w", err)
	}
	return nil
}

func cameraCoverRefreshTTL(streamState string, isRunning bool) time.Duration {
	switch {
	case isRunning || streamState == "online":
		return cameraCoverRefreshActiveTTL
	case streamState == "idle":
		return cameraCoverRefreshIdleTTL
	default:
		return cameraCoverRefreshDefaultTTL
	}
}

func cameraCoverRetryDelay(streamState string) time.Duration {
	if streamState == "idle" {
		return cameraCoverRetryIdleBackoff
	}
	return cameraCoverRetryBackoff
}

func handleCameraCover(c *gin.Context) {
	camID := c.Param("id")
	content, contentType, ok := cameraCovers.image(camID)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}

	c.Header("Cache-Control", "private, no-store")
	c.Data(http.StatusOK, contentType, content)
}
