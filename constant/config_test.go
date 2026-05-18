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
