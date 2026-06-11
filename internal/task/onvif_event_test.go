package task

import (
	"context"
	"testing"
	"time"

	"github.com/r0n9/camkeep/internal/onvif"
	"github.com/r0n9/camkeep/internal/service"
)

func TestIsOnvifMotionNotification(t *testing.T) {
	tests := []struct {
		name         string
		notification onvif.EventNotification
		want         bool
	}{
		{
			name: "standard motion alarm true",
			notification: onvif.EventNotification{
				Topic: "tns1:VideoSource/MotionAlarm",
				Data:  []onvif.EventItem{{Name: "State", Value: "true"}},
			},
			want: true,
		},
		{
			name: "rule engine motion true",
			notification: onvif.EventNotification{
				Topic: "tns1:RuleEngine/CellMotionDetector/Motion",
				Data:  []onvif.EventItem{{Name: "IsMotion", Value: "1"}},
			},
			want: true,
		},
		{
			name: "field detector objects inside true",
			notification: onvif.EventNotification{
				Topic: "tns1:RuleEngine/FieldDetector/ObjectsInside",
			},
			want: true,
		},
		{
			name: "field detector objects inside explicit true",
			notification: onvif.EventNotification{
				Topic: "tns1:RuleEngine/FieldDetector/ObjectsInside",
				Data:  []onvif.EventItem{{Name: "IsInside", Value: "true"}},
			},
			want: true,
		},
		{
			name: "field detector objects inside false",
			notification: onvif.EventNotification{
				Topic: "tns1:RuleEngine/FieldDetector/ObjectsInside",
				Data:  []onvif.EventItem{{Name: "IsInside", Value: "false"}},
			},
			want: false,
		},
		{
			name: "any rule engine topic is motion",
			notification: onvif.EventNotification{
				Topic: "tns1:RuleEngine/LineDetector/Crossed",
			},
			want: true,
		},
		{
			name: "count aggregation counter is not motion",
			notification: onvif.EventNotification{
				Topic: "tns1:RuleEngine/CountAggregation/Counter",
				Data:  []onvif.EventItem{{Name: "State", Value: "true"}},
			},
			want: false,
		},
		{
			name: "motion alarm false",
			notification: onvif.EventNotification{
				Topic: "tns1:VideoSource/MotionAlarm",
				Data:  []onvif.EventItem{{Name: "State", Value: "false"}},
			},
			want: false,
		},
		{
			name: "motion alarm without state is treated as trigger",
			notification: onvif.EventNotification{
				Topic: "tns1:VideoSource/MotionAlarm",
			},
			want: true,
		},
		{
			name: "non motion topic",
			notification: onvif.EventNotification{
				Topic: "tns1:Device/Trigger/DigitalInput",
				Data:  []onvif.EventItem{{Name: "State", Value: "true"}},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isOnvifMotionNotification(tt.notification); got != tt.want {
				t.Fatalf("expected %t, got %t", tt.want, got)
			}
		})
	}
}

func TestHandleOnvifEventNotificationPublishesMotionEvent(t *testing.T) {
	resetDetectionEventsForTest(t)

	eventAt := time.Now().Add(-time.Second)
	handleOnvifEventNotification("cam1", onvif.EventNotification{
		Topic:     "tns1:VideoSource/MotionAlarm",
		Operation: "Changed",
		At:        eventAt,
		Data:      []onvif.EventItem{{Name: "State", Value: "true"}},
	})

	event, ok := RecentDetectionEvent("cam1", EventTypeMotion, time.Now(), 5*time.Second)
	if !ok {
		t.Fatal("expected ONVIF motion event to be published")
	}
	if event.Source != "onvif-pullpoint" {
		t.Fatalf("expected ONVIF source, got %q", event.Source)
	}
	if event.Metadata["topic"] != "tns1:VideoSource/MotionAlarm" {
		t.Fatalf("expected topic metadata, got %+v", event.Metadata)
	}
}

func TestHandleOnvifEventNotificationIgnoresMotionFalse(t *testing.T) {
	resetDetectionEventsForTest(t)

	eventAt := time.Now().Add(-time.Second)
	handleOnvifEventNotification("cam1", onvif.EventNotification{
		Topic: "tns1:VideoSource/MotionAlarm",
		At:    eventAt,
		Data:  []onvif.EventItem{{Name: "State", Value: "false"}},
	})

	if _, ok := RecentDetectionEvent("cam1", EventTypeMotion, time.Now(), 5*time.Second); ok {
		t.Fatal("expected false motion event not to be published")
	}
}

func TestHandleOnvifEventNotificationIgnoresFieldDetectorObjectsOutside(t *testing.T) {
	resetDetectionEventsForTest(t)

	eventAt := time.Now().Add(-time.Second)
	handleOnvifEventNotification("cam1", onvif.EventNotification{
		Topic: "tns1:RuleEngine/FieldDetector/ObjectsInside",
		At:    eventAt,
		Data:  []onvif.EventItem{{Name: "IsInside", Value: "false"}},
	})

	if _, ok := RecentDetectionEvent("cam1", EventTypeMotion, time.Now(), 5*time.Second); ok {
		t.Fatal("expected ObjectsInside IsInside=false event not to be published")
	}
}

func TestHandleOnvifEventNotificationSuppressesInitialSnapshotMotion(t *testing.T) {
	resetDetectionEventsForTest(t)

	eventAt := time.Now().Add(-time.Second)
	handleOnvifEventNotificationWithOptions("cam1", onvif.EventNotification{
		Topic: "tns1:VideoSource/MotionAlarm",
		At:    eventAt,
	}, onvifEventHandleOptions{
		SuppressMotionPublish: true,
		SuppressReason:        "test startup snapshot",
	})

	if _, ok := RecentDetectionEvent("cam1", EventTypeMotion, time.Now(), 5*time.Second); ok {
		t.Fatal("expected suppressed initial snapshot motion not to be published")
	}
}

func TestShouldSuppressInitialOnvifMotionPublish(t *testing.T) {
	startedAt := time.Now()
	firstPullAt := startedAt.Add(time.Second)
	latePullAt := startedAt.Add(onvifEventInitialSnapshotWindow + time.Second)

	if !shouldSuppressInitialOnvifMotionPublish(onvif.EventNotification{
		Topic: "tns1:VideoSource/MotionAlarm",
	}, true, startedAt, firstPullAt) {
		t.Fatal("expected first PullPoint snapshot motion to be suppressed")
	}
	if shouldSuppressInitialOnvifMotionPublish(onvif.EventNotification{
		Topic:     "tns1:VideoSource/MotionAlarm",
		Operation: "Changed",
	}, true, startedAt, firstPullAt) {
		t.Fatal("expected explicit Changed event not to be suppressed")
	}
	if shouldSuppressInitialOnvifMotionPublish(onvif.EventNotification{
		Topic: "tns1:VideoSource/MotionAlarm",
	}, false, startedAt, firstPullAt) {
		t.Fatal("expected later pull messages not to be suppressed")
	}
	if shouldSuppressInitialOnvifMotionPublish(onvif.EventNotification{
		Topic: "tns1:VideoSource/MotionAlarm",
	}, true, startedAt, latePullAt) {
		t.Fatal("expected late first message not to be treated as startup snapshot")
	}
	if shouldSuppressInitialOnvifMotionPublish(onvif.EventNotification{
		Topic: "tns1:Device/Trigger/DigitalInput",
	}, true, startedAt, firstPullAt) {
		t.Fatal("expected non-motion topic not to be suppressed as motion")
	}
}

func TestHandleOnvifEventNotificationStoresLastEventForDiagnostics(t *testing.T) {
	resetDetectionEventsForTest(t)

	candidate := onvif.Candidate{
		ID:        "cam-diag",
		SourceURL: "onvif://admin:secret@192.0.2.20",
		Endpoint:  "http://192.0.2.20/onvif/device_service",
	}
	service.ReplaceOnvifCandidates([]onvif.Candidate{candidate})
	t.Cleanup(func() {
		service.ReplaceOnvifCandidates(nil)
	})

	eventAt := time.Now().Add(-time.Second)
	handleOnvifEventNotification("cam-diag", onvif.EventNotification{
		Topic:     "tns1:Device/Trigger/DigitalInput",
		Operation: "Changed",
		At:        eventAt,
		Source:    []onvif.EventItem{{Name: "InputToken", Value: "1"}},
		Data:      []onvif.EventItem{{Name: "LogicalState", Value: "true"}},
	})

	status, ok := service.GetOnvifStatus("cam-diag")
	if !ok {
		t.Fatal("expected ONVIF status")
	}
	if status.LastEvent == nil {
		t.Fatal("expected last ONVIF event to be stored")
	}
	if status.LastEvent.Topic != "tns1:Device/Trigger/DigitalInput" {
		t.Fatalf("expected diagnostic event topic, got %+v", status.LastEvent)
	}
	if status.LastEvent.Motion {
		t.Fatalf("expected non-motion diagnostic event, got %+v", status.LastEvent)
	}
	if status.LastEvent.Source != "InputToken=1" || status.LastEvent.Data != "LogicalState=true" {
		t.Fatalf("expected formatted event items, got %+v", status.LastEvent)
	}
}

func TestWaitOnvifPullPointReadyUsesAvailableCapabilityStatus(t *testing.T) {
	candidate := onvif.Candidate{
		ID:        "onvif-ready",
		SourceURL: "onvif://admin:secret@192.0.2.10",
		Endpoint:  "http://192.0.2.10/onvif/device_service",
	}
	service.ReplaceOnvifCandidates([]onvif.Candidate{candidate})
	t.Cleanup(func() {
		service.ReplaceOnvifCandidates(nil)
	})
	service.UpdateOnvifProbeResult(candidate, onvif.Capabilities{
		EventXAddr:       "http://192.0.2.10/onvif/events",
		PullPointSupport: true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	status, ok := waitOnvifPullPointReady(ctx, candidate)
	if !ok {
		t.Fatal("expected PullPoint-ready status")
	}
	if status.EventXAddr != "http://192.0.2.10/onvif/events" {
		t.Fatalf("expected event xaddr, got %q", status.EventXAddr)
	}
}

func TestWaitOnvifPullPointReadyStopsForUnsupportedPullPoint(t *testing.T) {
	candidate := onvif.Candidate{
		ID:        "onvif-no-pullpoint",
		SourceURL: "onvif://admin:secret@192.0.2.11",
		Endpoint:  "http://192.0.2.11/onvif/device_service",
	}
	service.ReplaceOnvifCandidates([]onvif.Candidate{candidate})
	t.Cleanup(func() {
		service.ReplaceOnvifCandidates(nil)
	})
	service.UpdateOnvifProbeResult(candidate, onvif.Capabilities{
		EventXAddr:       "http://192.0.2.11/onvif/events",
		PullPointSupport: false,
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if _, ok := waitOnvifPullPointReady(ctx, candidate); ok {
		t.Fatal("expected unsupported PullPoint to stop watcher")
	}
}
