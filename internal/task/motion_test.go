package task

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/r0n9/camkeep/constant"
	"github.com/r0n9/camkeep/internal/onvif"
	"github.com/r0n9/camkeep/internal/service"
)

func TestCompareMotionFramesDetectsMotion(t *testing.T) {
	prevFrame := make([]byte, motionDetectFrameWidth*motionDetectFrameHeight)
	currentFrame := make([]byte, motionDetectFrameWidth*motionDetectFrameHeight)

	changedPixels := int(float64(len(currentFrame))*motionDetectRatioThreshold) + 1
	for i := 0; i < changedPixels; i++ {
		currentFrame[i] = byte(motionDetectPixelThreshold + 1)
	}

	stats := compareMotionFrames(prevFrame, currentFrame, motionDetectPixelThreshold, motionDetectRatioThreshold)
	if !stats.Motion {
		t.Fatal("expected frame diff above ratio threshold to be detected as motion")
	}
	if stats.DiffPixels != changedPixels {
		t.Fatalf("expected %d changed pixels, got %d", changedPixels, stats.DiffPixels)
	}
}

func TestCompareMotionFramesIgnoresNoise(t *testing.T) {
	prevFrame := make([]byte, motionDetectFrameWidth*motionDetectFrameHeight)
	currentFrame := make([]byte, motionDetectFrameWidth*motionDetectFrameHeight)

	for i := range currentFrame {
		currentFrame[i] = byte(motionDetectPixelThreshold)
	}

	stats := compareMotionFrames(prevFrame, currentFrame, motionDetectPixelThreshold, motionDetectRatioThreshold)
	if stats.Motion {
		t.Fatal("expected changes at pixel threshold to be treated as noise")
	}
	if stats.DiffPixels != 0 {
		t.Fatalf("expected no pixels above threshold, got %d", stats.DiffPixels)
	}
}

func TestMotionRatioThresholdUsesConfiguredValue(t *testing.T) {
	threshold := motionRatioThreshold(constant.Camera{
		ID:                         "cam1",
		Mode:                       "normal",
		MotionDetect:               true,
		MotionDetectRatioThreshold: 0.05,
	})

	if threshold != 0.05 {
		t.Fatalf("expected configured threshold, got %f", threshold)
	}
}

func TestMotionDetectInputURLUsesConfiguredMotionURL(t *testing.T) {
	cam := constant.Camera{
		ID:        "cam1",
		MotionURL: " rtsp://example.local/substream ",
	}

	if got := motionDetectInputURL(cam); got != "rtsp://example.local/substream" {
		t.Fatalf("expected configured motion_url, got %q", got)
	}
}

func TestMotionMarkerFrameDiffUsesConfiguredMotionStreamAndThreshold(t *testing.T) {
	cam := constant.Camera{
		ID:                         "marker-frame-diff",
		Mode:                       "normal",
		MotionURL:                  " rtsp://example.local/marker-substream ",
		MotionMarkEnabled:          true,
		MotionMarkEventSource:      constant.MotionEventSourceFrameDiff,
		MotionDetectRatioThreshold: 0.07,
	}

	if !FrameDiffMotionDetectionEnabled(cam) {
		t.Fatal("expected frame diff detection to run for motion marker source")
	}
	if got := motionDetectInputURL(cam); got != "rtsp://example.local/marker-substream" {
		t.Fatalf("expected marker frame diff to use configured motion_url, got %q", got)
	}
	if got := motionRatioThreshold(cam); got != 0.07 {
		t.Fatalf("expected marker frame diff to use configured threshold, got %f", got)
	}
}

func TestMotionDetectInputURLFallsBackToGo2rtcStream(t *testing.T) {
	cam := constant.Camera{ID: "cam1"}

	want := "rtsp://" + constant.DefaultGo2rtcHost + ":8554/cam1"
	if got := motionDetectInputURL(cam); got != want {
		t.Fatalf("expected fallback URL %q, got %q", want, got)
	}
}

func TestMotionRecordingEnabledOnlyForNormalMode(t *testing.T) {
	if !motionRecordingEnabled(constant.Camera{Mode: "normal", MotionDetect: true}) {
		t.Fatal("expected motion recording enabled for normal mode")
	}
	if motionRecordingEnabled(constant.Camera{Mode: "timelapse", MotionDetect: true}) {
		t.Fatal("expected motion recording disabled for timelapse mode")
	}
	if motionRecordingEnabled(constant.Camera{Mode: "normal"}) {
		t.Fatal("expected motion recording disabled by default")
	}
}

func TestFrameDiffMotionDetectionEnabledRespectsEventSource(t *testing.T) {
	base := constant.Camera{Mode: "normal", MotionDetect: true}

	if !FrameDiffMotionDetectionEnabled(base) {
		t.Fatal("expected default event source to use frame diff")
	}
	if FrameDiffMotionDetectionEnabled(constant.Camera{Mode: "normal", MotionDetect: true, MotionEventSource: constant.MotionEventSourceONVIF}) {
		t.Fatal("expected ONVIF-only event source to skip frame diff")
	}
	if !FrameDiffMotionDetectionEnabled(constant.Camera{Mode: "normal", MotionDetect: true, MotionEventSource: constant.MotionEventSourceAuto}) {
		t.Fatal("expected auto event source to keep frame diff fallback task")
	}
	if !FrameDiffMotionDetectionEnabled(constant.Camera{Mode: "normal", MotionMarkEnabled: true}) {
		t.Fatal("expected motion marker auto source to keep frame diff fallback task")
	}
	if FrameDiffMotionDetectionEnabled(constant.Camera{Mode: "normal", MotionMarkEnabled: true, MotionMarkEventSource: constant.MotionEventSourceONVIF}) {
		t.Fatal("expected ONVIF-only marker source to skip frame diff")
	}
	if FrameDiffMotionDetectionEnabled(constant.Camera{
		Mode:                  "normal",
		MotionDetect:          true,
		MotionEventSource:     constant.MotionEventSourceONVIF,
		MotionMarkEnabled:     true,
		MotionMarkEventSource: constant.MotionEventSourceFrameDiff,
	}) {
		t.Fatal("expected marker generation to stay disabled while motion recording mode is enabled")
	}
}

func TestRecordingWindowEnabled(t *testing.T) {
	tests := []struct {
		name        string
		control     string
		inTimeRange bool
		want        bool
	}{
		{name: "auto in range", inTimeRange: true, want: true},
		{name: "auto out of range", inTimeRange: false, want: false},
		{name: "manual start ignores schedule", control: "start", inTimeRange: false, want: true},
		{name: "manual stop blocks schedule", control: "stop", inTimeRange: true, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := recordingWindowEnabled(tt.control, tt.inTimeRange); got != tt.want {
				t.Fatalf("expected %t, got %t", tt.want, got)
			}
		})
	}
}

func TestMotionDetectionShouldRunRespectsOverrideStop(t *testing.T) {
	cam := constant.Camera{ID: "motion-override-stop", Mode: "normal", MotionDetect: true}
	setOverridesForTest(t, map[string]string{cam.ID: "stop"})
	setStreamStateForTest(t, cam.ID, "online")

	if motionDetectionShouldRun(cam) {
		t.Fatal("expected manual stop override to block motion detection")
	}
}

func TestGetOverrideDefaultsToAuto(t *testing.T) {
	setOverridesForTest(t, map[string]string{"manual-start": "start"})

	if got := GetOverride("manual-start"); got != "start" {
		t.Fatalf("expected start override, got %q", got)
	}
	if got := GetOverride("missing"); got != "auto" {
		t.Fatalf("expected missing override to be auto, got %q", got)
	}
}

func TestMotionDetectionShouldRunAllowsIdleStream(t *testing.T) {
	cam := constant.Camera{ID: "motion-idle-stream", Mode: "normal", MotionDetect: true}
	setOverridesForTest(t, nil)
	setStreamStateForTest(t, cam.ID, "idle")

	if !motionDetectionShouldRun(cam) {
		t.Fatal("expected idle stream to allow motion detection")
	}
}

func TestMotionDetectionShouldRunBlocksOfflineStream(t *testing.T) {
	cam := constant.Camera{ID: "motion-offline-stream", Mode: "normal", MotionDetect: true}
	setOverridesForTest(t, nil)
	setStreamStateForTest(t, cam.ID, "offline")

	if motionDetectionShouldRun(cam) {
		t.Fatal("expected offline stream to block motion detection")
	}
}

func TestMotionDetectionShouldRunAutoFallsBackWhenOnvifUnavailable(t *testing.T) {
	cam := constant.Camera{
		ID:                "motion-auto-fallback",
		Mode:              "normal",
		MotionDetect:      true,
		MotionEventSource: constant.MotionEventSourceAuto,
		RecordTime:        "00:00-23:59",
	}
	setOverridesForTest(t, nil)
	setStreamStateForTest(t, cam.ID, "online")
	service.ReplaceOnvifCandidates(nil)
	t.Cleanup(func() { service.ReplaceOnvifCandidates(nil) })

	if !motionDetectionShouldRunAt(cam, time.Now()) {
		t.Fatal("expected auto mode to run frame diff when ONVIF event source is unavailable")
	}
}

func TestMotionDetectionShouldRunAutoStopsWhenOnvifHealthy(t *testing.T) {
	cam := constant.Camera{
		ID:                "motion-auto-onvif",
		Mode:              "normal",
		MotionDetect:      true,
		MotionEventSource: constant.MotionEventSourceAuto,
		RecordTime:        "00:00-23:59",
	}
	candidate := onvif.Candidate{
		ID:        cam.ID,
		SourceURL: "onvif://admin:secret@example/onvif/device_service",
		Endpoint:  "http://example/onvif/device_service",
	}
	setOverridesForTest(t, nil)
	setStreamStateForTest(t, cam.ID, "online")
	service.ReplaceOnvifCandidates([]onvif.Candidate{candidate})
	t.Cleanup(func() { service.ReplaceOnvifCandidates(nil) })
	service.UpdateOnvifProbeResult(candidate, onvif.Capabilities{
		EventXAddr:       "http://example/onvif/events",
		PullPointSupport: true,
	})
	now := time.Now()
	service.UpdateOnvifEventListenerListening(cam.ID, now)

	if motionDetectionShouldRunAt(cam, now) {
		t.Fatal("expected auto mode to stop frame diff while ONVIF event source is healthy")
	}
}

func TestMotionDetectTaskDoesNotResetOnvifEventWhenFrameDiffIdle(t *testing.T) {
	resetDetectionEventsForTest(t)
	cam := constant.Camera{
		ID:                "motion-auto-onvif-event",
		Mode:              "normal",
		MotionDetect:      true,
		MotionEventSource: constant.MotionEventSourceAuto,
		RecordTime:        "00:00-23:59",
	}
	candidate := onvif.Candidate{
		ID:        cam.ID,
		SourceURL: "onvif://admin:secret@example/onvif/device_service",
		Endpoint:  "http://example/onvif/device_service",
	}
	setOverridesForTest(t, nil)
	setStreamStateForTest(t, cam.ID, "online")
	service.ReplaceOnvifCandidates([]onvif.Candidate{candidate})
	t.Cleanup(func() { service.ReplaceOnvifCandidates(nil) })
	service.UpdateOnvifProbeResult(candidate, onvif.Capabilities{
		EventXAddr:       "http://example/onvif/events",
		PullPointSupport: true,
	})
	now := time.Now()
	service.UpdateOnvifEventListenerListening(cam.ID, now)
	PublishDetectionEvent(DetectionEvent{
		CameraID: cam.ID,
		Type:     EventTypeMotion,
		Source:   "onvif-pullpoint",
		At:       now,
	})

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go MotionDetectTask(ctx, &wg, cam)
	time.Sleep(50 * time.Millisecond)
	cancel()
	wg.Wait()

	if _, ok := RecentDetectionEvent(cam.ID, EventTypeMotion, time.Now(), motionRecordIdleTimeout); !ok {
		t.Fatal("expected ONVIF motion event to survive while frame diff detector is idle")
	}
}

func TestMotionMarkerDetectionShouldRunAutoFallbackWhenOnvifUnavailable(t *testing.T) {
	cam := constant.Camera{
		ID:                    "marker-auto-fallback",
		Mode:                  "normal",
		MotionMarkEnabled:     true,
		MotionMarkEventSource: constant.MotionEventSourceAuto,
		RecordTime:            "00:00-23:59",
	}
	setOverridesForTest(t, nil)
	setStreamStateForTest(t, cam.ID, "online")
	service.ReplaceOnvifCandidates(nil)
	t.Cleanup(func() { service.ReplaceOnvifCandidates(nil) })

	if !motionDetectionShouldRunAt(cam, time.Now()) {
		t.Fatal("expected marker auto mode to run frame diff when ONVIF event source is unavailable")
	}
}

func TestMotionMarkerDetectionShouldStopFrameDiffWhenOnvifHealthy(t *testing.T) {
	cam := constant.Camera{
		ID:                    "marker-auto-onvif",
		Mode:                  "normal",
		MotionMarkEnabled:     true,
		MotionMarkEventSource: constant.MotionEventSourceAuto,
		RecordTime:            "00:00-23:59",
	}
	candidate := onvif.Candidate{
		ID:        cam.ID,
		SourceURL: "onvif://admin:secret@example/onvif/device_service",
		Endpoint:  "http://example/onvif/device_service",
	}
	setOverridesForTest(t, nil)
	setStreamStateForTest(t, cam.ID, "online")
	service.ReplaceOnvifCandidates([]onvif.Candidate{candidate})
	t.Cleanup(func() { service.ReplaceOnvifCandidates(nil) })
	service.UpdateOnvifProbeResult(candidate, onvif.Capabilities{
		EventXAddr:       "http://example/onvif/events",
		PullPointSupport: true,
	})
	now := time.Now()
	service.UpdateOnvifEventListenerListening(cam.ID, now)

	if motionDetectionShouldRunAt(cam, now) {
		t.Fatal("expected marker auto mode to stop frame diff while ONVIF event source is healthy")
	}
}

func TestMotionDetectionShouldRunOnvifOnlySkipsFrameDiff(t *testing.T) {
	cam := constant.Camera{
		ID:                "motion-onvif-only",
		Mode:              "normal",
		MotionDetect:      true,
		MotionEventSource: constant.MotionEventSourceONVIF,
		RecordTime:        "00:00-23:59",
	}
	setOverridesForTest(t, nil)
	setStreamStateForTest(t, cam.ID, "online")

	if motionDetectionShouldRunAt(cam, time.Now()) {
		t.Fatal("expected ONVIF-only source to skip frame diff")
	}
}

func TestNewEventRecordSessionAppliesPreRecord(t *testing.T) {
	detectedAt := time.Date(2026, 5, 12, 10, 0, 0, 0, time.Local)
	session := newEventRecordSession(EventTypeMotion, detectedAt)
	if !session.StartTime.Equal(detectedAt.Add(-motionTimeShiftPreRecord)) {
		t.Fatalf("expected prerecord start %s, got %s", detectedAt.Add(-motionTimeShiftPreRecord), session.StartTime)
	}
	if session.EventType != EventTypeMotion {
		t.Fatalf("expected event type %q, got %q", EventTypeMotion, session.EventType)
	}
}

func TestMotionTimeShiftRecordingWindowConstants(t *testing.T) {
	if motionRecordIdleTimeout != 15*time.Second {
		t.Fatalf("expected motion idle timeout 15s, got %s", motionRecordIdleTimeout)
	}
	if motionTimeShiftSegmentDuration != time.Minute {
		t.Fatalf("expected Time-Shift segment duration 1m, got %s", motionTimeShiftSegmentDuration)
	}
	if motionTimeShiftSegmentCount != 3 {
		t.Fatalf("expected Time-Shift segment count 3, got %d", motionTimeShiftSegmentCount)
	}
}

func TestMotionTimeShiftExitedNoSpace(t *testing.T) {
	noSpaceCmd := exec.Command("sh", "-c", "exit 228")
	if err := noSpaceCmd.Run(); !motionTimeShiftExitedNoSpace(err) {
		t.Fatalf("expected exit 228 to be treated as ENOSPC, got %v", err)
	}

	otherCmd := exec.Command("sh", "-c", "exit 1")
	if err := otherCmd.Run(); motionTimeShiftExitedNoSpace(err) {
		t.Fatalf("expected exit 1 not to be treated as ENOSPC, got %v", err)
	}
	if motionTimeShiftExitedNoSpace(nil) {
		t.Fatal("expected nil error not to be treated as ENOSPC")
	}
}

func TestEnableMotionTimeShiftTmpFallbackUsesTempDir(t *testing.T) {
	camID := "test-timeshift-fallback"
	resetMotionTimeShiftTmpFallbackForTest(t, camID)

	enableMotionTimeShiftTmpFallback(camID)

	got := motionTimeShiftDir(camID)
	want := filepath.Join(os.TempDir(), motionTimeShiftBufferBaseName, camID)
	if got != want {
		t.Fatalf("expected fallback dir %q, got %q", want, got)
	}
	t.Cleanup(func() {
		os.RemoveAll(want)
	})
}

func TestParseMotionTimeShiftSegmentStart(t *testing.T) {
	start, ok := parseMotionTimeShiftSegmentStart("loop_20260512_100001.ts")
	if !ok {
		t.Fatal("expected segment filename parsed")
	}
	want := time.Date(2026, 5, 12, 10, 0, 1, 0, time.Local)
	if !start.Equal(want) {
		t.Fatalf("expected %s, got %s", want, start)
	}
	if _, ok := parseMotionTimeShiftSegmentStart("chunk_000.ts"); ok {
		t.Fatal("expected non-timeshift filename ignored")
	}
}

func TestMotionTimeShiftClipsAcrossSegments(t *testing.T) {
	camID := "test-timeshift-clips"
	bufferDir := motionTimeShiftDir(camID)
	t.Cleanup(func() {
		os.RemoveAll(bufferDir)
	})
	if err := os.RemoveAll(bufferDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bufferDir, 0755); err != nil {
		t.Fatal(err)
	}

	baseTime := time.Date(2026, 5, 12, 10, 0, 0, 0, time.Local)
	createTimeShiftTestSegment(t, bufferDir, baseTime)
	createTimeShiftTestSegment(t, bufferDir, baseTime.Add(motionTimeShiftSegmentDuration))

	clips, err := motionTimeShiftClips(camID, baseTime.Add(50*time.Second), baseTime.Add(70*time.Second), baseTime.Add(70*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(clips) != 2 {
		t.Fatalf("expected 2 clips across segment boundary, got %d", len(clips))
	}
	if got := clips[0].end.Sub(clips[0].start); got != 10*time.Second {
		t.Fatalf("expected first clip 10s, got %s", got)
	}
	if got := clips[1].end.Sub(clips[1].start); got != 10*time.Second {
		t.Fatalf("expected second clip 10s, got %s", got)
	}
}

func TestPrepareMotionTimeShiftClipsKeepsLiveTSSource(t *testing.T) {
	base := time.Date(2026, 5, 12, 10, 0, 0, 0, time.Local)
	clips := []motionTimeShiftClip{
		{
			source: motionTimeShiftSegment{
				path:  "live.ts",
				start: base,
				end:   base.Add(5 * time.Second),
				live:  true,
			},
			start: base.Add(1 * time.Second),
			end:   base.Add(4 * time.Second),
		},
	}

	prepared, err := prepareMotionTimeShiftClips(clips)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared) != 1 {
		t.Fatalf("expected 1 prepared clip, got %d", len(prepared))
	}
	if !prepared[0].source.live || prepared[0].source.path != "live.ts" {
		t.Fatalf("expected live TS source preserved, got %+v", prepared[0].source)
	}
}

func TestPrepareMotionTimeShiftClipsRejectsAllInvalidDurations(t *testing.T) {
	base := time.Date(2026, 5, 12, 10, 0, 0, 0, time.Local)
	clips := []motionTimeShiftClip{
		{
			source: motionTimeShiftSegment{
				path:  "invalid.ts",
				start: base,
				end:   base.Add(5 * time.Second),
			},
			start: base.Add(2 * time.Second),
			end:   base.Add(2 * time.Second),
		},
	}

	if _, err := prepareMotionTimeShiftClips(clips); err == nil || err.Error() != "所有动检片段时长均无效" {
		t.Fatalf("expected invalid-duration error, got %v", err)
	}
}

func TestMotionTimeShiftSegmentsMarksCurrentSegmentLive(t *testing.T) {
	camID := "test-timeshift-live"
	bufferDir := motionTimeShiftDir(camID)
	t.Cleanup(func() {
		os.RemoveAll(bufferDir)
	})
	if err := os.RemoveAll(bufferDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bufferDir, 0755); err != nil {
		t.Fatal(err)
	}

	baseTime := time.Date(2026, 5, 12, 10, 0, 0, 0, time.Local)
	createTimeShiftTestSegment(t, bufferDir, baseTime)

	segments, err := motionTimeShiftSegments(camID, baseTime.Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segments))
	}
	if !segments[0].live {
		t.Fatal("expected current segment to be marked live")
	}
}

func TestPruneMotionTimeShiftSegmentsKeepsNewestSegments(t *testing.T) {
	camID := "test-timeshift-prune"
	bufferDir := motionTimeShiftDir(camID)
	t.Cleanup(func() {
		os.RemoveAll(bufferDir)
	})
	if err := os.RemoveAll(bufferDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bufferDir, 0755); err != nil {
		t.Fatal(err)
	}

	baseTime := time.Date(2026, 5, 12, 10, 0, 0, 0, time.Local)
	var paths []string
	for i := 0; i < motionTimeShiftSegmentCount+2; i++ {
		paths = append(paths, createTimeShiftTestSegment(t, bufferDir, baseTime.Add(time.Duration(i)*motionTimeShiftSegmentDuration)))
	}

	pruneMotionTimeShiftSegments(camID, time.Time{})
	for i, path := range paths {
		_, err := os.Stat(path)
		if i < 2 && !os.IsNotExist(err) {
			t.Fatalf("expected old segment %s removed, err=%v", path, err)
		}
		if i >= 2 && err != nil {
			t.Fatalf("expected newer segment %s kept, err=%v", path, err)
		}
	}
}

func TestFormatSeconds(t *testing.T) {
	if got := formatSeconds(2 * time.Second); got != "2" {
		t.Fatalf("expected integer seconds, got %q", got)
	}
	if got := formatSeconds(1500 * time.Millisecond); got != "1.500" {
		t.Fatalf("expected millisecond precision, got %q", got)
	}
}

func createTimeShiftTestSegment(t *testing.T, dir string, start time.Time) string {
	t.Helper()
	name := motionTimeShiftFilePrefix + start.Format(motionTimeShiftTimeLayout) + motionTimeShiftSegmentExt
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("segment"), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func setOverridesForTest(t *testing.T, values map[string]string) {
	t.Helper()

	overrideMux.Lock()
	oldOverrides := overrides
	overrides = make(map[string]string, len(values))
	for camID, action := range values {
		overrides[camID] = action
	}
	overrideMux.Unlock()

	t.Cleanup(func() {
		overrideMux.Lock()
		overrides = oldOverrides
		overrideMux.Unlock()
	})
}

func resetMotionTimeShiftTmpFallbackForTest(t *testing.T, camID string) {
	t.Helper()

	motionTimeShiftFallbackMux.Lock()
	oldValue, hadOldValue := motionTimeShiftTmpFallback[camID]
	delete(motionTimeShiftTmpFallback, camID)
	motionTimeShiftFallbackMux.Unlock()

	t.Cleanup(func() {
		motionTimeShiftFallbackMux.Lock()
		if hadOldValue {
			motionTimeShiftTmpFallback[camID] = oldValue
		} else {
			delete(motionTimeShiftTmpFallback, camID)
		}
		motionTimeShiftFallbackMux.Unlock()
	})
}

func setStreamStateForTest(t *testing.T, camID, state string) {
	t.Helper()

	service.StatusMux.Lock()
	oldStatus, hadStatus := service.StatusMap[camID]
	service.StatusMap[camID] = &service.CameraStatus{StreamState: state}
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
