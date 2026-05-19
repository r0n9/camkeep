package service

import (
	"errors"
	"testing"

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
