package service

import (
	"errors"
	"testing"
	"time"

	"github.com/r0n9/camkeep/internal/onvif"
)

func TestUpdateOnvifProbeResultStoresCapabilities(t *testing.T) {
	candidate := onvif.Candidate{
		ID:        "front",
		SourceURL: "onvif://admin:secret@example/onvif/device_service",
		Endpoint:  "http://example/onvif/device_service",
		Username:  "admin",
	}
	ReplaceOnvifCandidates([]onvif.Candidate{candidate})
	t.Cleanup(func() { ReplaceOnvifCandidates(nil) })

	MarkOnvifProbeStarted(candidate)
	UpdateOnvifProbeResult(candidate, onvif.Capabilities{
		MediaXAddr:       "http://example/onvif/media",
		PTZXAddr:         "http://example/onvif/ptz",
		ImagingXAddr:     "http://example/onvif/imaging",
		EventXAddr:       "http://example/onvif/events",
		PullPointSupport: true,
		ProfileToken:     "profile_1",
		ProfileName:      "Main",
		VideoSourceToken: "video_1",
	})

	status, ok := GetOnvifStatus("front")
	if !ok {
		t.Fatal("expected ONVIF status")
	}
	if status.CapabilityState != OnvifStateAvailable {
		t.Fatalf("expected capability available, got %q", status.CapabilityState)
	}
	if status.PTZState != OnvifStateAvailable || status.ImagingState != OnvifStateAvailable || status.EventState != OnvifStateAvailable {
		t.Fatalf("unexpected PTZ/Imaging/Event states: %+v", status)
	}
	if status.SourceURL != "onvif://admin:redacted@example/onvif/device_service" {
		t.Fatalf("expected redacted source URL, got %q", status.SourceURL)
	}
	if !status.PullPointSupport {
		t.Fatal("expected pull point support to be stored")
	}
	if status.ImagingXAddr != "http://example/onvif/imaging" || status.VideoSourceToken != "video_1" {
		t.Fatalf("expected imaging capability to be stored, got %+v", status)
	}
}

func TestUpdateOnvifLastEventStoresAndPreservesAcrossCandidateRefresh(t *testing.T) {
	candidate := onvif.Candidate{
		ID:        "front",
		SourceURL: "onvif://admin:secret@example/onvif/device_service",
		Endpoint:  "http://example/onvif/device_service",
	}
	ReplaceOnvifCandidates([]onvif.Candidate{candidate})
	t.Cleanup(func() { ReplaceOnvifCandidates(nil) })

	eventAt := time.Date(2026, 6, 8, 10, 30, 0, 0, time.UTC)
	UpdateOnvifLastEvent("front", OnvifEventSnapshot{
		Topic:       "tns1:VideoSource/MotionAlarm",
		Operation:   "Changed",
		At:          eventAt,
		ReceivedAt:  eventAt.Add(time.Second),
		Data:        "State=true",
		Motion:      true,
		MotionTopic: true,
	})

	status, ok := GetOnvifStatus("front")
	if !ok {
		t.Fatal("expected ONVIF status")
	}
	if status.LastEvent == nil {
		t.Fatal("expected last event to be stored")
	}
	if status.LastEvent.Topic != "tns1:VideoSource/MotionAlarm" || !status.LastEvent.Motion {
		t.Fatalf("unexpected last event: %+v", status.LastEvent)
	}
	if !status.MotionEventVerified {
		t.Fatal("expected motion event topic to be marked verified")
	}

	ReplaceOnvifCandidates([]onvif.Candidate{candidate})
	status, ok = GetOnvifStatus("front")
	if !ok || status.LastEvent == nil {
		t.Fatal("expected last event to survive candidate refresh")
	}
	if !status.LastEvent.At.Equal(eventAt) {
		t.Fatalf("expected event time %s, got %s", eventAt, status.LastEvent.At)
	}
}

func TestUpdateOnvifLastEventKeepsRecentEventsNewestFirst(t *testing.T) {
	candidate := onvif.Candidate{
		ID:        "front-recent",
		SourceURL: "onvif://admin:secret@example/onvif/device_service",
		Endpoint:  "http://example/onvif/device_service",
	}
	ReplaceOnvifCandidates([]onvif.Candidate{candidate})
	t.Cleanup(func() { ReplaceOnvifCandidates(nil) })

	base := time.Date(2026, 6, 8, 10, 30, 0, 0, time.UTC)
	for i, topic := range []string{"topic-1", "topic-2", "topic-3", "topic-4"} {
		UpdateOnvifLastEvent("front-recent", OnvifEventSnapshot{
			Topic:      topic,
			At:         base.Add(time.Duration(i) * time.Second),
			ReceivedAt: base.Add(time.Duration(i) * time.Second),
		})
	}

	status, ok := GetOnvifStatus("front-recent")
	if !ok {
		t.Fatal("expected ONVIF status")
	}
	if len(status.RecentEvents) != 3 {
		t.Fatalf("expected 3 recent events, got %d", len(status.RecentEvents))
	}
	if got := status.RecentEvents[0].Topic; got != "topic-4" {
		t.Fatalf("expected newest event first, got %q", got)
	}
	if got := status.RecentEvents[2].Topic; got != "topic-2" {
		t.Fatalf("expected oldest retained event topic-2, got %q", got)
	}

	ReplaceOnvifCandidates([]onvif.Candidate{candidate})
	status, ok = GetOnvifStatus("front-recent")
	if !ok || len(status.RecentEvents) != 3 {
		t.Fatalf("expected recent events to survive candidate refresh, got %+v", status.RecentEvents)
	}
}

func TestOnvifEventSourceUsableRequiresHealthyListener(t *testing.T) {
	candidate := onvif.Candidate{
		ID:        "event-source",
		SourceURL: "onvif://admin:secret@example/onvif/device_service",
		Endpoint:  "http://example/onvif/device_service",
	}
	ReplaceOnvifCandidates([]onvif.Candidate{candidate})
	t.Cleanup(func() { ReplaceOnvifCandidates(nil) })

	now := time.Date(2026, 6, 8, 11, 0, 0, 0, time.UTC)
	UpdateOnvifProbeResult(candidate, onvif.Capabilities{
		EventXAddr:       "http://example/onvif/events",
		PullPointSupport: true,
	})

	if OnvifEventSourceUsable("event-source", now) {
		t.Fatal("expected event source to require listener success")
	}

	UpdateOnvifEventListenerListening("event-source", now.Add(-time.Second))
	if !OnvifEventSourceUsable("event-source", now) {
		t.Fatal("expected healthy listener to make ONVIF event source usable")
	}

	UpdateOnvifEventListenerListening("event-source", now.Add(-OnvifEventSourceHealthWindow-time.Second))
	if OnvifEventSourceUsable("event-source", now) {
		t.Fatal("expected stale listener success to be unusable")
	}

	UpdateOnvifEventListenerError("event-source", errors.New("pull failed"))
	if OnvifEventSourceUsable("event-source", now) {
		t.Fatal("expected listener error to make event source unusable")
	}
}

func TestUpdateOnvifProbeErrorIgnoresStaleCandidate(t *testing.T) {
	current := onvif.Candidate{
		ID:        "front",
		SourceURL: "onvif://admin:new@example/onvif/device_service",
		Endpoint:  "http://example/onvif/device_service",
	}
	stale := onvif.Candidate{
		ID:        "front",
		SourceURL: "onvif://admin:old@example/onvif/device_service",
		Endpoint:  "http://example/onvif/device_service",
	}
	ReplaceOnvifCandidates([]onvif.Candidate{current})
	t.Cleanup(func() { ReplaceOnvifCandidates(nil) })

	UpdateOnvifProbeError(stale, errors.New("stale error"))

	status, ok := GetOnvifStatus("front")
	if !ok {
		t.Fatal("expected ONVIF status")
	}
	if status.CapabilityState != OnvifStateNotProbed {
		t.Fatalf("expected stale result to be ignored, got %+v", status)
	}
}
