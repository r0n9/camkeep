package onvif

import (
	"testing"

	"github.com/r0n9/camkeep/constant"
)

func TestCandidateFromDirectONVIFStream(t *testing.T) {
	candidate, ok := CandidateFromCamera(constant.Camera{
		ID:        "front",
		StreamURL: "onvif://admin:secret@192.168.1.10:8000",
	}, nil)
	if !ok {
		t.Fatal("expected ONVIF candidate")
	}

	if candidate.SourceType != SourceTypeDirect {
		t.Fatalf("expected direct source type, got %q", candidate.SourceType)
	}
	if candidate.ManagedByGo2rtc {
		t.Fatal("expected direct ONVIF camera not to be marked go2rtc managed")
	}
	if candidate.Endpoint != "http://192.168.1.10:8000/onvif/device_service" {
		t.Fatalf("unexpected endpoint: %q", candidate.Endpoint)
	}
	if candidate.Username != "admin" {
		t.Fatalf("expected username admin, got %q", candidate.Username)
	}
	if candidate.Password != "secret" {
		t.Fatalf("expected password to be parsed")
	}
}

func TestCandidateFromManagedGo2rtcONVIFSource(t *testing.T) {
	candidate, ok := CandidateFromCamera(constant.Camera{
		ID:        "managed",
		StreamURL: constant.ManagedByGo2rtcURL,
	}, []string{
		"rtsp://example/live",
		"ffmpeg:onvif://admin:secret@example/onvif/device_service?subtype=0",
	})
	if !ok {
		t.Fatal("expected managed ONVIF candidate")
	}

	if candidate.SourceType != SourceTypeGo2rtc {
		t.Fatalf("expected go2rtc source type, got %q", candidate.SourceType)
	}
	if !candidate.ManagedByGo2rtc {
		t.Fatal("expected managed go2rtc candidate")
	}
	if candidate.SourceURL != "onvif://admin:secret@example/onvif/device_service?subtype=0" {
		t.Fatalf("unexpected ONVIF source: %q", candidate.SourceURL)
	}
	if candidate.Endpoint != "http://example/onvif/device_service" {
		t.Fatalf("unexpected endpoint: %q", candidate.Endpoint)
	}
}

func TestCandidateIgnoresNonONVIFSources(t *testing.T) {
	_, ok := CandidateFromCamera(constant.Camera{
		ID:        "rtsp",
		StreamURL: "rtsp://example/live",
	}, []string{"onvif://admin:secret@example/onvif/device_service"})
	if ok {
		t.Fatal("expected explicit RTSP camera not to use go2rtc ONVIF sources")
	}
}

func TestMaskSourceURLRedactsPassword(t *testing.T) {
	got := MaskSourceURL("onvif://admin:secret@example/onvif/device_service")
	want := "onvif://admin:redacted@example/onvif/device_service"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
