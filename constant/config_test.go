package constant

import "testing"

func TestCameraEffectiveStreamURLPrefersStreamURL(t *testing.T) {
	cam := Camera{
		StreamURL: " rtsp://new.example/live ",
		RTSPUrl:   "rtsp://old.example/live",
	}

	if got := cam.EffectiveStreamURL(); got != "rtsp://new.example/live" {
		t.Fatalf("expected stream_url to win, got %q", got)
	}
}

func TestCameraEffectiveStreamURLFallsBackToRTSPURL(t *testing.T) {
	cam := Camera{RTSPUrl: " rtsp://old.example/live "}

	if got := cam.EffectiveStreamURL(); got != "rtsp://old.example/live" {
		t.Fatalf("expected legacy rtsp_url fallback, got %q", got)
	}
}

func TestCameraManagedByGo2rtcUsesEffectiveStreamURL(t *testing.T) {
	cases := []struct {
		name string
		cam  Camera
		want bool
	}{
		{
			name: "stream_url sentinel",
			cam:  Camera{StreamURL: ManagedByGo2rtcURL},
			want: true,
		},
		{
			name: "legacy rtsp_url sentinel",
			cam:  Camera{RTSPUrl: ManagedByGo2rtcURL},
			want: true,
		},
		{
			name: "stream_url overrides legacy sentinel",
			cam:  Camera{StreamURL: "rtsp://new.example/live", RTSPUrl: ManagedByGo2rtcURL, AutoDiscovered: true},
			want: false,
		},
		{
			name: "auto discovered without explicit source",
			cam:  Camera{AutoDiscovered: true},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CameraManagedByGo2rtc(tc.cam); got != tc.want {
				t.Fatalf("expected %t, got %t", tc.want, got)
			}
		})
	}
}

func TestNormalizeMotionEventSource(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "", want: MotionEventSourceFrameDiff},
		{input: " FRAME_DIFF ", want: MotionEventSourceFrameDiff},
		{input: "onvif", want: MotionEventSourceONVIF},
		{input: "AUTO", want: MotionEventSourceAuto},
		{input: "unknown", want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := NormalizeMotionEventSource(tt.input); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestValidMotionEventSource(t *testing.T) {
	for _, source := range []string{"", MotionEventSourceFrameDiff, MotionEventSourceONVIF, MotionEventSourceAuto} {
		if !ValidMotionEventSource(source) {
			t.Fatalf("expected %q to be valid", source)
		}
	}
	if ValidMotionEventSource("unknown") {
		t.Fatal("expected unknown motion event source to be invalid")
	}
}
