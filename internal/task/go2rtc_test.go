package task

import (
	"testing"
	"time"
)

func TestDefaultPortForSchemeSupportsONVIF(t *testing.T) {
	if got := defaultPortForScheme("onvif"); got != "80" {
		t.Fatalf("expected ONVIF default port 80, got %q", got)
	}
}

func TestGo2rtcStreamStateUsesConfiguredProbeWhenProducerHasError(t *testing.T) {
	resetStreamRecoveryForTest(t)

	camData := map[string]interface{}{
		"producers": []interface{}{
			map[string]interface{}{
				"url":   "rtsp://stale.example/live",
				"error": "i/o timeout",
			},
		},
	}
	var probed []string
	now := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC)

	got := go2rtcStreamState("cam1", camData, " rtsp://camera.example/live ", "offline", now, func(rawURL string) bool {
		probed = append(probed, rawURL)
		return true
	})

	if got != "idle" {
		t.Fatalf("expected reachable producer error to become idle, got %q", got)
	}
	if len(probed) != 1 || probed[0] != "rtsp://camera.example/live" {
		t.Fatalf("expected configured URL to be probed, got %#v", probed)
	}
}

func TestGo2rtcStreamStateKeepsProducerErrorOfflineWhenProbeFails(t *testing.T) {
	resetStreamRecoveryForTest(t)

	camData := map[string]interface{}{
		"producers": []interface{}{
			map[string]interface{}{
				"url":   "rtsp://camera.example/live",
				"error": "i/o timeout",
			},
		},
	}
	now := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC)

	got := go2rtcStreamState("cam1", camData, "rtsp://camera.example/live", "offline", now, func(string) bool {
		return false
	})

	if got != "offline" {
		t.Fatalf("expected unreachable producer error to stay offline, got %q", got)
	}
}

func TestGo2rtcStreamStateIdleProducerRechecksTCPAfterProbeInterval(t *testing.T) {
	resetStreamRecoveryForTest(t)

	camData := map[string]interface{}{
		"producers": []interface{}{
			map[string]interface{}{
				"url": "rtsp://camera.example/live",
			},
		},
	}
	now := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC)
	probeCount := 0

	if got := go2rtcStreamState("cam1", camData, "rtsp://camera.example/live", "offline", now, func(string) bool {
		probeCount++
		return true
	}); got != "idle" {
		t.Fatalf("expected reachable idle producer to be idle, got %q", got)
	}
	if got := go2rtcStreamState("cam1", camData, "rtsp://camera.example/live", "idle", now.Add(streamIdleProbeInterval-time.Second), func(string) bool {
		probeCount++
		return false
	}); got != "idle" {
		t.Fatalf("expected cached idle producer to stay idle before probe interval, got %q", got)
	}
	if probeCount != 1 {
		t.Fatalf("expected cached idle state to avoid a second probe, got %d probes", probeCount)
	}
	if got := go2rtcStreamState("cam1", camData, "rtsp://camera.example/live", "idle", now.Add(streamIdleProbeInterval+time.Second), func(string) bool {
		probeCount++
		return false
	}); got != "offline" {
		t.Fatalf("expected idle producer to become offline after failed recheck, got %q", got)
	}
	if probeCount != 2 {
		t.Fatalf("expected expired idle cache to trigger a second probe, got %d probes", probeCount)
	}
}

func TestGo2rtcStreamStateBacksOffAfterReachableErrorProbeWindow(t *testing.T) {
	resetStreamRecoveryForTest(t)

	camData := map[string]interface{}{
		"producers": []interface{}{
			map[string]interface{}{
				"url":   "rtsp://camera.example/live",
				"error": "i/o timeout",
			},
		},
	}
	now := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC)
	tcpAlive := func(string) bool { return true }

	if got := go2rtcStreamState("cam1", camData, "rtsp://camera.example/live", "offline", now, tcpAlive); got != "idle" {
		t.Fatalf("expected first reachable error to open idle probe window, got %q", got)
	}
	if got := go2rtcStreamState("cam1", camData, "rtsp://camera.example/live", "idle", now.Add(streamRecoveryProbeWindow-time.Second), tcpAlive); got != "idle" {
		t.Fatalf("expected active probe window to stay idle, got %q", got)
	}
	if got := go2rtcStreamState("cam1", camData, "rtsp://camera.example/live", "idle", now.Add(streamRecoveryProbeWindow+time.Second), tcpAlive); got != "offline" {
		t.Fatalf("expected expired probe window to enter backoff, got %q", got)
	}
	if got := go2rtcStreamState("cam1", camData, "rtsp://camera.example/live", "offline", now.Add(streamRecoveryProbeWindow+2*time.Second), tcpAlive); got != "offline" {
		t.Fatalf("expected backoff period to stay offline, got %q", got)
	}
	if got := go2rtcStreamState("cam1", camData, "rtsp://camera.example/live", "offline", now.Add(streamRecoveryProbeWindow+streamRecoveryBaseBackoff+2*time.Second), tcpAlive); got != "idle" {
		t.Fatalf("expected retry after backoff to reopen idle probe window, got %q", got)
	}
}

func TestGo2rtcStreamStateActiveProducerWinsOverError(t *testing.T) {
	resetStreamRecoveryForTest(t)

	camData := map[string]interface{}{
		"producers": []interface{}{
			map[string]interface{}{
				"url":   "rtsp://camera.example/live",
				"error": "i/o timeout",
			},
			map[string]interface{}{
				"url":        "rtsp://camera.example/live",
				"bytes_recv": float64(1024),
			},
		},
	}
	probed := false
	now := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC)

	got := go2rtcStreamState("cam1", camData, "rtsp://camera.example/live", "offline", now, func(string) bool {
		probed = true
		return false
	})

	if got != "online" {
		t.Fatalf("expected active producer to be online, got %q", got)
	}
	if probed {
		t.Fatal("did not expect TCP probe when producer is active")
	}
}

func TestGo2rtcStreamStateOnlineClearsRecoveryBackoff(t *testing.T) {
	resetStreamRecoveryForTest(t)

	errorData := map[string]interface{}{
		"producers": []interface{}{
			map[string]interface{}{
				"url":   "rtsp://camera.example/live",
				"error": "i/o timeout",
			},
		},
	}
	onlineData := map[string]interface{}{
		"producers": []interface{}{
			map[string]interface{}{
				"url":        "rtsp://camera.example/live",
				"bytes_recv": float64(2048),
			},
		},
	}
	now := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC)
	tcpAlive := func(string) bool { return true }

	if got := go2rtcStreamState("cam1", errorData, "rtsp://camera.example/live", "offline", now, tcpAlive); got != "idle" {
		t.Fatalf("expected first reachable error to open idle probe window, got %q", got)
	}
	if got := go2rtcStreamState("cam1", errorData, "rtsp://camera.example/live", "idle", now.Add(streamRecoveryProbeWindow+time.Second), tcpAlive); got != "offline" {
		t.Fatalf("expected expired probe window to enter backoff, got %q", got)
	}
	if got := go2rtcStreamState("cam1", onlineData, "rtsp://camera.example/live", "offline", now.Add(streamRecoveryProbeWindow+2*time.Second), tcpAlive); got != "online" {
		t.Fatalf("expected active stream to become online, got %q", got)
	}
	if got := go2rtcStreamState("cam1", errorData, "rtsp://camera.example/live", "offline", now.Add(streamRecoveryProbeWindow+3*time.Second), tcpAlive); got != "idle" {
		t.Fatalf("expected online state to clear previous backoff, got %q", got)
	}
}

func resetStreamRecoveryForTest(t *testing.T) {
	t.Helper()

	streamRecoveryMux.Lock()
	oldRecoveries := streamRecoveries
	streamRecoveries = make(map[string]streamRecoveryState)
	streamRecoveryMux.Unlock()

	streamIdleProbeMux.Lock()
	oldIdleProbes := streamIdleProbes
	streamIdleProbes = make(map[string]streamIdleProbeState)
	streamIdleProbeMux.Unlock()

	t.Cleanup(func() {
		streamRecoveryMux.Lock()
		streamRecoveries = oldRecoveries
		streamRecoveryMux.Unlock()

		streamIdleProbeMux.Lock()
		streamIdleProbes = oldIdleProbes
		streamIdleProbeMux.Unlock()
	})
}
