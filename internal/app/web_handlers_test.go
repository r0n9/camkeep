package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/r0n9/camkeep/constant"
	"github.com/r0n9/camkeep/internal/onvif"
	"github.com/r0n9/camkeep/internal/service"
)

func TestHandleStatusIncludesRecordOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)

	camID := "status-override-auto"
	deleteStatusForAppTest(t, camID)
	service.UpdateStatus(camID, false, "normal", "09:00-18:00")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	handleStatus(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var payload map[string]map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if got := payload[camID]["record_override"]; got != "auto" {
		t.Fatalf("expected record_override auto, got %v", got)
	}
}

func TestFilterRecordEntriesDefaultKeepsLatestSevenAvailableDates(t *testing.T) {
	entries := []recordEntry{
		testRecordEntry(t, "2026-05-03"),
		testRecordEntry(t, "2026-04-29"),
		testRecordEntry(t, "2026-05-12"),
		testRecordEntry(t, "2026-05-07"),
		testRecordEntry(t, "2026-05-01"),
		testRecordEntry(t, "2026-05-11"),
		testRecordEntry(t, "2026-05-09"),
		testRecordEntry(t, "2026-05-05"),
	}

	got := recordFilePaths(filterRecordEntries(entries, recordDateRange{}))
	want := []string{
		"cam1/2026-05-12/2026-05-12.ts",
		"cam1/2026-05-11/2026-05-11.ts",
		"cam1/2026-05-09/2026-05-09.ts",
		"cam1/2026-05-07/2026-05-07.ts",
		"cam1/2026-05-05/2026-05-05.ts",
		"cam1/2026-05-03/2026-05-03.ts",
		"cam1/2026-05-01/2026-05-01.ts",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestFilterRecordEntriesExplicitRangeDoesNotBackfill(t *testing.T) {
	entries := []recordEntry{
		testRecordEntry(t, "2026-05-12"),
		testRecordEntry(t, "2026-05-10"),
		testRecordEntry(t, "2026-05-04"),
	}
	dateRange, err := parseRecordDateRange("2026-05-09", "2026-05-12")
	if err != nil {
		t.Fatal(err)
	}

	got := recordFilePaths(filterRecordEntries(entries, dateRange))
	want := []string{
		"cam1/2026-05-12/2026-05-12.ts",
		"cam1/2026-05-10/2026-05-10.ts",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseRecordDateRangeLimitsSevenConsecutiveDays(t *testing.T) {
	if _, err := parseRecordDateRange("2026-05-01", "2026-05-07"); err != nil {
		t.Fatalf("expected 7-day range to pass, got %v", err)
	}
	if _, err := parseRecordDateRange("2026-05-01", "2026-05-08"); err == nil {
		t.Fatal("expected range longer than 7 days to fail")
	}
	if _, err := parseRecordDateRange("2026-05-02", ""); err == nil {
		t.Fatal("expected partial range to fail")
	}
	if _, err := parseRecordDateRange("2026-05-02", "2026-05-01"); err == nil {
		t.Fatal("expected inverted range to fail")
	}
}

func TestParseRecordDateFromPath(t *testing.T) {
	got, ok := parseRecordDateFromPath("cam1/2026-05-12/cam1_2026-05-12_10-20-30.mp4")
	if !ok {
		t.Fatal("expected date to be parsed from path")
	}
	want := time.Date(2026, 5, 12, 0, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("expected %s, got %s", want, got)
	}
	if _, ok := parseRecordDateFromPath("cam1/2026-99-99/bad.mp4"); ok {
		t.Fatal("expected invalid date to be ignored")
	}
}

func TestParseConfigYAMLValidatesKnownShape(t *testing.T) {
	validYAML := []byte(`
daily_merge:
  enabled: true
  time: "03:30"
cameras:
  - id: "cam_01"
    stream_url: "rtsp://example/live"
    motion_url: "rtsp://example/substream"
    record_time: "00:00-23:59"
`)

	cfg, err := parseConfigYAML(validYAML)
	if err != nil {
		t.Fatalf("expected valid config to pass, got %v", err)
	}
	if !cfg.DailyMerge.Enabled || cfg.DailyMerge.Time != "03:30" || len(cfg.Cameras) != 1 {
		t.Fatalf("unexpected parsed config: %+v", cfg)
	}
	if cfg.Cameras[0].MotionURL != "rtsp://example/substream" {
		t.Fatalf("expected motion_url to be parsed, got %q", cfg.Cameras[0].MotionURL)
	}
	if cfg.Cameras[0].EffectiveStreamURL() != "rtsp://example/live" {
		t.Fatalf("expected stream_url to be parsed, got %q", cfg.Cameras[0].EffectiveStreamURL())
	}
}

func TestParseConfigYAMLSupportsLegacyRTSPURL(t *testing.T) {
	cfg, err := parseConfigYAML([]byte(`
cameras:
  - id: "cam_01"
    rtsp_url: "rtsp://legacy.example/live"
`))
	if err != nil {
		t.Fatalf("expected legacy rtsp_url to pass, got %v", err)
	}
	if got := cfg.Cameras[0].EffectiveStreamURL(); got != "rtsp://legacy.example/live" {
		t.Fatalf("expected legacy rtsp_url fallback, got %q", got)
	}
}

func TestParseConfigYAMLStreamURLTakesPriorityOverRTSPURL(t *testing.T) {
	cfg, err := parseConfigYAML([]byte(`
cameras:
  - id: "cam_01"
    stream_url: "rtsp://new.example/live"
    rtsp_url: "rtsp://old.example/live"
`))
	if err != nil {
		t.Fatalf("expected config with both stream_url and rtsp_url to pass, got %v", err)
	}
	if got := cfg.Cameras[0].EffectiveStreamURL(); got != "rtsp://new.example/live" {
		t.Fatalf("expected stream_url to win, got %q", got)
	}
}

func TestMarkGo2rtcManagedCamerasUsesEffectiveStreamURLSentinel(t *testing.T) {
	cfg := constant.Config{
		Cameras: []constant.Camera{
			{ID: "managed", StreamURL: constant.ManagedByGo2rtcURL},
			{ID: "legacy-managed", RTSPUrl: constant.ManagedByGo2rtcURL},
			{ID: "direct", StreamURL: "rtsp://example/live", RTSPUrl: constant.ManagedByGo2rtcURL, AutoDiscovered: true},
		},
	}

	markGo2rtcManagedCameras(&cfg)

	if !cfg.Cameras[0].AutoDiscovered {
		t.Fatal("expected managed_by_go2rtc camera to be marked auto_discovered")
	}
	if !cfg.Cameras[1].AutoDiscovered {
		t.Fatal("expected legacy managed_by_go2rtc camera to be marked auto_discovered")
	}
	if cfg.Cameras[2].AutoDiscovered {
		t.Fatal("expected explicit stream_url to clear stale auto_discovered")
	}
}

func TestParseConfigYAMLRejectsUnknownFields(t *testing.T) {
	_, err := parseConfigYAML([]byte(`
cameras:
  - id: "cam_01"
    stream_url: "rtsp://example/live"
    typo_field: true
`))
	if err == nil {
		t.Fatal("expected unknown field to fail")
	}
	if !strings.Contains(err.Error(), "typo_field") {
		t.Fatalf("expected error to mention unknown field, got %v", err)
	}
}

func TestParseConfigYAMLRejectsEmptyConfig(t *testing.T) {
	if _, err := parseConfigYAML([]byte("  \n")); err == nil {
		t.Fatal("expected empty config to fail")
	}
}

func TestBuildGo2rtcStreamScanResponseUsesConfigSourceLabels(t *testing.T) {
	scan := buildGo2rtcStreamScanResponse(map[string]interface{}{
		"streams": map[string]interface{}{
			"managed_rtsp": map[string]interface{}{
				"producers": []interface{}{
					map[string]interface{}{"url": "webrtc://runtime-state-should-not-win"},
				},
			},
			"onvif_cam": map[string]interface{}{
				"producers": []interface{}{
					map[string]interface{}{"url": "rtsp://runtime-state-should-not-win"},
				},
			},
			"wrapped_ffmpeg": map[string]interface{}{
				"producers": []interface{}{
					map[string]interface{}{"url": "rtsp://runtime-state-should-not-win"},
				},
			},
		},
	}, map[string]bool{"managed_rtsp": true}, map[string][]string{
		"managed_rtsp":   {"rtsp://example/live"},
		"onvif_cam":      {"onvif://admin:password@example/onvif/device"},
		"wrapped_ffmpeg": {"ffmpeg:rtsp://example/live"},
		"config_only":    {"exec:ffmpeg -re -f lavfi -i testsrc -f rtsp {output}"},
	})

	if got := scan.Streams["managed_rtsp"].SourceLabel; got != "RTSP" {
		t.Fatalf("expected RTSP source label, got %q", got)
	}
	if got := scan.Streams["onvif_cam"].SourceLabel; got != "ONVIF" {
		t.Fatalf("expected ONVIF source label, got %q", got)
	}
	if !scan.Streams["onvif_cam"].ONVIFEnabled {
		t.Fatal("expected ONVIF stream to be marked ONVIF enabled")
	}
	if scan.Streams["managed_rtsp"].ONVIFEnabled {
		t.Fatal("expected RTSP stream not to be marked ONVIF enabled")
	}
	if got := scan.Streams["wrapped_ffmpeg"].SourceLabel; got != "FFmpeg / RTSP" {
		t.Fatalf("expected FFmpeg / RTSP source label, got %q", got)
	}
	if got := scan.Streams["config_only"].SourceLabel; got != "Exec" {
		t.Fatalf("expected Exec source label, got %q", got)
	}
	if !scan.Streams["managed_rtsp"].Managed {
		t.Fatal("expected managed stream to be marked managed")
	}
	if got := len(scan.Unmanaged); got != 3 {
		t.Fatalf("expected three unmanaged streams, got %d", got)
	}
}

func TestReadGo2rtcConfigStreamSources(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "go2rtc.yaml")
	if err := os.WriteFile(configPath, []byte(`
streams:
  rtsp_cam: rtsp://example/live
  ffmpeg_cam:
    - ffmpeg:rtsp://example/live
  onvif_cam:
    - onvif://admin:password@example/onvif/device
`), 0644); err != nil {
		t.Fatal(err)
	}

	sources, ok := readGo2rtcConfigStreamSources(configPath)
	if !ok {
		t.Fatal("expected go2rtc config sources to be parsed")
	}
	if got := formatGo2rtcSourceLabels(sources["rtsp_cam"]); got != "RTSP" {
		t.Fatalf("expected RTSP label, got %q", got)
	}
	if got := formatGo2rtcSourceLabels(sources["ffmpeg_cam"]); got != "FFmpeg / RTSP" {
		t.Fatalf("expected FFmpeg / RTSP label, got %q", got)
	}
	if got := formatGo2rtcSourceLabels(sources["onvif_cam"]); got != "ONVIF" {
		t.Fatalf("expected ONVIF label, got %q", got)
	}
}

func TestHandleOnvifStatusListsCandidates(t *testing.T) {
	service.ReplaceOnvifCandidates([]onvif.Candidate{
		{
			ID:              "front",
			SourceType:      onvif.SourceTypeDirect,
			SourceURL:       "onvif://admin:secret@example/onvif/device_service",
			Endpoint:        "http://example/onvif/device_service",
			Username:        "admin",
			ManagedByGo2rtc: false,
		},
	})
	t.Cleanup(func() {
		service.ReplaceOnvifCandidates(nil)
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	handleOnvifStatus(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var payload struct {
		Count   int                   `json:"count"`
		Devices []service.OnvifStatus `json:"devices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Count != 1 || len(payload.Devices) != 1 {
		t.Fatalf("expected one ONVIF device, got %+v", payload)
	}
	if got := payload.Devices[0].SourceURL; strings.Contains(got, "secret") {
		t.Fatalf("expected source URL to redact password, got %q", got)
	}
	if payload.Devices[0].PTZState != service.OnvifStateNotProbed {
		t.Fatalf("expected PTZ state not_probed, got %q", payload.Devices[0].PTZState)
	}
}

func TestHandlePTZStatusRequiresONVIFCandidate(t *testing.T) {
	setCurrentConfigForAppTest(t, constant.Config{
		Cameras: []constant.Camera{{ID: "plain", StreamURL: "rtsp://example/live"}},
	})
	service.ReplaceOnvifCandidates(nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "plain"}}
	handlePTZStatus(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-ONVIF camera, got %d", w.Code)
	}
}

func TestParseConfigYAMLRejectsInvalidCameraFormat(t *testing.T) {
	_, err := parseConfigYAML([]byte(`
cameras:
  - id: "cam_01"
    format: "avi"
`))
	if err == nil {
		t.Fatal("expected invalid format to fail")
	}
	if !strings.Contains(err.Error(), "format") || !strings.Contains(err.Error(), "cam_01") {
		t.Fatalf("expected error to mention camera and format, got %v", err)
	}
}

func TestParseConfigYAMLRejectsInvalidCameraMode(t *testing.T) {
	_, err := parseConfigYAML([]byte(`
cameras:
  - id: "cam_01"
    mode: "Normal"
`))
	if err == nil {
		t.Fatal("expected invalid mode to fail")
	}
	if !strings.Contains(err.Error(), "mode") || !strings.Contains(err.Error(), "cam_01") {
		t.Fatalf("expected error to mention camera and mode, got %v", err)
	}
}

func TestParseConfigYAMLRejectsInvalidCameraNumbers(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "negative segment duration",
			yaml: `
cameras:
  - id: "cam_01"
    segment_duration: -1
`,
			want: "segment_duration",
		},
		{
			name: "invalid retention days",
			yaml: `
cameras:
  - id: "cam_01"
    retention_days: -2
`,
			want: "retention_days",
		},
		{
			name: "negative min size",
			yaml: `
cameras:
  - id: "cam_01"
    min_size_kb: -1
`,
			want: "min_size_kb",
		},
		{
			name: "invalid motion threshold",
			yaml: `
cameras:
  - id: "cam_01"
    motionDetectRatioThreshold: 1.5
`,
			want: "motionDetectRatioThreshold",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseConfigYAML([]byte(tt.yaml))
			if err == nil {
				t.Fatal("expected invalid config to fail")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error to mention %s, got %v", tt.want, err)
			}
		})
	}
}

func testRecordEntry(t *testing.T, dateKey string) recordEntry {
	t.Helper()

	date, err := parseRecordDate(dateKey)
	if err != nil {
		t.Fatal(err)
	}
	return recordEntry{
		date:    date,
		dateKey: dateKey,
		file: recordFile{
			Name: dateKey + ".ts",
			Path: "cam1/" + dateKey + "/" + dateKey + ".ts",
		},
	}
}

func recordFilePaths(files []recordFile) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	return paths
}

func deleteStatusForAppTest(t *testing.T, camID string) {
	t.Helper()

	service.StatusMux.Lock()
	oldStatus, hadStatus := service.StatusMap[camID]
	delete(service.StatusMap, camID)
	service.StatusMux.Unlock()

	t.Cleanup(func() {
		service.StatusMux.Lock()
		if hadStatus {
			service.StatusMap[camID] = oldStatus
		} else {
			delete(service.StatusMap, camID)
		}
		service.StatusMux.Unlock()
	})
}

func setCurrentConfigForAppTest(t *testing.T, cfg constant.Config) {
	t.Helper()

	constant.ConfigMux.Lock()
	oldConfig := currentConfig
	currentConfig = cfg
	constant.ConfigMux.Unlock()

	t.Cleanup(func() {
		constant.ConfigMux.Lock()
		currentConfig = oldConfig
		constant.ConfigMux.Unlock()
	})
}
