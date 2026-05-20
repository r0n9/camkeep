package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/r0n9/camkeep/internal/service"
)

func TestCameraCoverStoreTouchFetchesSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var requestedPath string
	var requestedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg-data"))
	}))
	defer server.Close()

	store := newCameraCoverStore()
	store.baseURL = server.URL
	store.recordDir = t.TempDir()

	store.touch("cam-01", "online", false)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if snapshot := store.snapshot("cam-01"); snapshot.Ready {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	snapshot := store.snapshot("cam-01")
	if !snapshot.Ready {
		t.Fatal("expected snapshot to be cached")
	}
	if snapshot.Version == 0 {
		t.Fatal("expected snapshot version to be assigned")
	}

	content, contentType, ok := store.image("cam-01")
	if !ok {
		t.Fatal("expected cached image to be readable")
	}
	if string(content) != "jpeg-data" {
		t.Fatalf("expected cached image data, got %q", string(content))
	}
	if contentType != "image/jpeg" {
		t.Fatalf("expected image/jpeg content type, got %q", contentType)
	}
	coverPath := filepath.Join(store.recordDir, "cam-01", cameraCoverFileName)
	diskContent, err := os.ReadFile(coverPath)
	if err != nil {
		t.Fatalf("expected cover to be persisted: %v", err)
	}
	if string(diskContent) != "jpeg-data" {
		t.Fatalf("expected persisted cover data, got %q", string(diskContent))
	}

	if requestedPath != "/api/frame.jpeg" {
		t.Fatalf("expected go2rtc frame path, got %q", requestedPath)
	}
	if requestedQuery != "height=180&src=cam-01&width=320" {
		t.Fatalf("unexpected snapshot query: %q", requestedQuery)
	}
}

func TestCameraCoverStoreFallsBackToFFmpeg(t *testing.T) {
	gin.SetMode(gin.TestMode)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	store := newCameraCoverStore()
	store.baseURL = server.URL
	store.recordDir = t.TempDir()
	store.ffmpegRun = func(ctx context.Context, args ...string) ([]byte, error) {
		return []byte("ffmpeg-data"), nil
	}

	store.touch("cam-ffmpeg", "online", false)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if snapshot := store.snapshot("cam-ffmpeg"); snapshot.Ready {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	content, contentType, ok := store.image("cam-ffmpeg")
	if !ok {
		t.Fatal("expected ffmpeg fallback image to be cached")
	}
	if string(content) != "ffmpeg-data" {
		t.Fatalf("expected ffmpeg fallback data, got %q", string(content))
	}
	if contentType != "image/jpeg" {
		t.Fatalf("expected fallback content type image/jpeg, got %q", contentType)
	}
}

func TestCameraCoverStoreIdleWithoutCoverStillRefreshes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("idle-cover"))
	}))
	defer server.Close()

	store := newCameraCoverStore()
	store.baseURL = server.URL
	store.recordDir = t.TempDir()

	store.touch("cam-idle", "idle", false)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && calls.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() == 0 {
		t.Fatal("expected idle camera without cover to refresh")
	}
}

func TestCameraCoverStoreLoadsPersistedSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recordDir := t.TempDir()
	coverDir := filepath.Join(recordDir, "cam-disk")
	if err := os.MkdirAll(coverDir, 0755); err != nil {
		t.Fatal(err)
	}
	coverPath := filepath.Join(coverDir, cameraCoverFileName)
	if err := os.WriteFile(coverPath, []byte("disk-cover"), 0644); err != nil {
		t.Fatal(err)
	}

	store := newCameraCoverStore()
	store.recordDir = recordDir

	snapshot := store.snapshot("cam-disk")
	if !snapshot.Ready {
		t.Fatal("expected persisted cover to be ready")
	}

	content, contentType, ok := store.image("cam-disk")
	if !ok {
		t.Fatal("expected persisted cover to be readable")
	}
	if string(content) != "disk-cover" {
		t.Fatalf("expected disk cover data, got %q", string(content))
	}
	if contentType != "image/jpeg" {
		t.Fatalf("expected image/jpeg content type, got %q", contentType)
	}
}

func TestHandleCameraCoverServesCachedImage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := newDisabledCameraCoverStore()
	storeSeedCameraCover(store, "cam-cover", []byte("cached-cover"), "image/jpeg", 123, time.Now())
	swapCameraCoverStoreForTest(t, store)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "cam-cover"}}

	handleCameraCover(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("expected image/jpeg, got %q", got)
	}
	if body := w.Body.String(); body != "cached-cover" {
		t.Fatalf("expected cached cover body, got %q", body)
	}
}

func TestCameraCoverStoreTouchDisabledSkipsFetch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ignored"))
	}))
	defer server.Close()

	store := newDisabledCameraCoverStore()
	store.baseURL = server.URL
	store.touch("cam-skip", "online", false)

	time.Sleep(50 * time.Millisecond)

	if got := calls.Load(); got != 0 {
		t.Fatalf("expected disabled store to skip fetch, got %d calls", got)
	}
}

func TestCameraCoverTaskRefreshesStatusMapCovers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	camID := "cover-task-refresh"
	deleteStatusForAppTest(t, camID)
	service.UpdateStatus(camID, false, "normal", "09:00-18:00")
	service.UpdateOnlineStatus(camID, "online")

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("task-cover"))
	}))
	defer server.Close()

	store := newCameraCoverStore()
	store.baseURL = server.URL
	store.recordDir = t.TempDir()
	swapCameraCoverStoreForTest(t, store)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		CameraCoverTask(ctx)
	}()
	defer func() {
		cancel()
		<-done
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && calls.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() == 0 {
		t.Fatal("expected cover task to trigger refresh")
	}
}

func newDisabledCameraCoverStore() *cameraCoverStore {
	store := newCameraCoverStore()
	store.enabled = false
	return store
}

func swapCameraCoverStoreForTest(t *testing.T, store *cameraCoverStore) {
	t.Helper()

	oldStore := cameraCovers
	cameraCovers = store

	t.Cleanup(func() {
		cameraCovers = oldStore
	})
}

func storeSeedCameraCover(store *cameraCoverStore, camID string, content []byte, contentType string, version int64, updatedAt time.Time) {
	store.mu.Lock()
	defer store.mu.Unlock()

	entry := store.ensureEntryLocked(camID)
	entry.Content = append([]byte(nil), content...)
	entry.ContentType = contentType
	entry.Version = version
	entry.UpdatedAt = updatedAt
}
