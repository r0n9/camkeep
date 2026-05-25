package task

import (
	"testing"
	"time"
)

func TestRecentDetectionEventReturnsLatestWithinWindow(t *testing.T) {
	resetDetectionEventsForTest(t)

	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.Local)
	PublishDetectionEvent(DetectionEvent{
		CameraID: "cam1",
		Type:     EventTypeMotion,
		Source:   "test",
		At:       now.Add(-2 * time.Second),
		Metadata: map[string]string{"kind": "first"},
	})
	PublishDetectionEvent(DetectionEvent{
		CameraID: "cam1",
		Type:     EventTypeMotion,
		Source:   "test",
		At:       now.Add(-1 * time.Second),
		Metadata: map[string]string{"kind": "latest"},
	})

	event, ok := RecentDetectionEvent("cam1", EventTypeMotion, now, 5*time.Second)
	if !ok {
		t.Fatal("expected recent motion event")
	}
	if event.Metadata["kind"] != "latest" {
		t.Fatalf("expected latest event metadata, got %+v", event.Metadata)
	}
}

func TestRecentDetectionEventIgnoresExpiredEvent(t *testing.T) {
	resetDetectionEventsForTest(t)

	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.Local)
	PublishDetectionEvent(DetectionEvent{
		CameraID: "cam1",
		Type:     EventTypeMotion,
		At:       now.Add(-10 * time.Second),
	})

	if _, ok := RecentDetectionEvent("cam1", EventTypeMotion, now, 5*time.Second); ok {
		t.Fatal("expected expired event to be ignored")
	}
}

func TestResetCameraDetectionEvents(t *testing.T) {
	resetDetectionEventsForTest(t)

	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.Local)
	PublishDetectionEvent(DetectionEvent{
		CameraID: "cam1",
		Type:     EventTypeMotion,
		At:       now,
	})
	ResetCameraDetectionEvents("cam1")

	if _, ok := RecentDetectionEvent("cam1", EventTypeMotion, now, 5*time.Second); ok {
		t.Fatal("expected reset camera events to clear recent event")
	}
}

func TestMotionDetectedRecentlyUsesDetectionEvents(t *testing.T) {
	resetDetectionEventsForTest(t)

	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.Local)
	markMotionDetected("cam1", now.Add(-time.Second))
	if !motionDetectedRecently("cam1", now) {
		t.Fatal("expected recent motion event to trigger recording")
	}

	resetMotionDetected("cam1")
	if motionDetectedRecently("cam1", now) {
		t.Fatal("expected reset motion event to stop triggering recording")
	}
}

func resetDetectionEventsForTest(t *testing.T) {
	t.Helper()

	detectionEventMux.Lock()
	oldEvents := detectionEvents
	detectionEvents = make(map[string]map[string]DetectionEvent)
	detectionEventMux.Unlock()

	t.Cleanup(func() {
		detectionEventMux.Lock()
		detectionEvents = oldEvents
		detectionEventMux.Unlock()
	})
}
