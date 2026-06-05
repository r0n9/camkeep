package task

import (
	"testing"
	"time"

	"github.com/r0n9/camkeep/internal/onvif"
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
