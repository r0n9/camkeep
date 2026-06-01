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

func TestCheckCameraTCPAliveAssumesReachableForGo2rtcNativeScheme(t *testing.T) {
	// xiaomi:// 等 go2rtc 原生 scheme 没有标准 TCP 端口，应视为可达交给真实拉流裁决，
	// 否则设备断电恢复后会永久卡在 offline、无法自动续录。
	nativeURLs := []string{
		"xiaomi://1234567890:cn@192.168.1.123?did=9876543210&model=isa.camera.hlc7",
		"tapo://admin:pass@192.168.1.50",
		"gb28181://192.168.1.60",
	}
	for _, raw := range nativeURLs {
		if !checkCameraTCPAlive(raw) {
			t.Fatalf("expected go2rtc-native scheme to be assumed reachable, got false for %q", raw)
		}
	}

	// 没有 scheme 的非法地址仍判不可达。
	if checkCameraTCPAlive("not-a-valid-url") {
		t.Fatal("expected schemeless garbage URL to be unreachable")
	}
}

func TestGo2rtcStreamStateXiaomiIdleProducerRecovers(t *testing.T) {
	resetStreamRecoveryForTest(t)

	// 复现小米接入：设备断电恢复后，go2rtc 上只剩一个干瘪的 xiaomi:// producer、无消费者、无数据。
	// 此前由于无法对 xiaomi scheme 做端口探活，会一直判 offline，导致普通/动检录制无法自动续上。
	camData := map[string]interface{}{
		"producers": []interface{}{
			map[string]interface{}{
				"url": "xiaomi://1234567890:cn@192.168.1.123?did=9876543210&model=isa.camera.hlc7",
			},
		},
	}
	now := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC)

	got := go2rtcStreamState("xiaomiCam", camData, "managed_by_go2rtc", "offline", now, checkCameraTCPAlive)
	if got != "idle" {
		t.Fatalf("expected xiaomi idle producer to become idle so recording can re-pull, got %q", got)
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
