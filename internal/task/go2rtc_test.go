package task

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r0n9/camkeep/constant"
)

func TestPrepareGo2rtcConfigMigratesLegacyFile(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "go2rtc.yaml")
	configPath := filepath.Join(dir, "config", "go2rtc.yaml")
	legacyContent := []byte("streams:\n  old: rtsp://example/live\n")

	if err := os.WriteFile(legacyPath, legacyContent, 0600); err != nil {
		t.Fatal(err)
	}

	if err := prepareGo2rtcConfig(legacyPath, configPath); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(legacyContent) {
		t.Fatalf("expected migrated content %q, got %q", legacyContent, got)
	}

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("expected migrated mode 0600, got %v", info.Mode().Perm())
	}
}

func TestPrepareGo2rtcConfigDoesNotOverwriteExistingConfig(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "go2rtc.yaml")
	configPath := filepath.Join(dir, "config", "go2rtc.yaml")

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte("legacy\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("current\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := prepareGo2rtcConfig(legacyPath, configPath); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "current\n" {
		t.Fatalf("expected existing config to be preserved, got %q", got)
	}
}

func TestPrepareGo2rtcConfigAllowsMissingLegacyFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config", "go2rtc.yaml")

	if err := prepareGo2rtcConfig(filepath.Join(dir, "go2rtc.yaml"), configPath); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Dir(configPath)); err != nil {
		t.Fatalf("expected config directory to exist: %v", err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("expected config file to remain absent, got err=%v", err)
	}
}

func TestInitGo2rtcStreamsUsesStreamURLBeforeRTSPURL(t *testing.T) {
	oldClient := httpClient
	defer func() { httpClient = oldClient }()

	var registeredSrc string
	httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.Method {
			case http.MethodGet:
				return testHTTPResponse(http.StatusOK, "{}"), nil
			case http.MethodDelete:
				return testHTTPResponse(http.StatusOK, ""), nil
			case http.MethodPut:
				registeredSrc = req.URL.Query().Get("src")
				return testHTTPResponse(http.StatusOK, ""), nil
			default:
				return testHTTPResponse(http.StatusMethodNotAllowed, ""), nil
			}
		}),
	}

	InitGo2rtcStreams(constant.Config{
		Cameras: []constant.Camera{{
			ID:        "cam_01",
			StreamURL: "rtsp://new.example/live",
			RTSPUrl:   "rtsp://old.example/live",
		}},
	})

	if registeredSrc != "rtsp://new.example/live" {
		t.Fatalf("expected go2rtc src to use stream_url, got %q", registeredSrc)
	}
}

func TestUnwrapGo2rtcNetworkURL(t *testing.T) {
	cases := map[string]string{
		"rtsp://example/live":        "rtsp://example/live",
		"ffmpeg:rtsp://example/live": "rtsp://example/live",
		"mjpeg:http://example/live":  "http://example/live",
		"exec:camera":                "exec:camera",
	}

	for input, want := range cases {
		if got := unwrapGo2rtcNetworkURL(input); got != want {
			t.Fatalf("unwrapGo2rtcNetworkURL(%q) = %q, want %q", input, got, want)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func testHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestDefaultPortForScheme(t *testing.T) {
	cases := map[string]string{
		"rtsp":  "554",
		"rtsps": "322",
		"http":  "80",
		"https": "443",
		"exec":  "",
	}

	for input, want := range cases {
		if got := defaultPortForScheme(input); got != want {
			t.Fatalf("defaultPortForScheme(%q) = %q, want %q", input, got, want)
		}
	}
}
