package task

import (
	"context"
	"testing"
	"time"

	"github.com/r0n9/camkeep/constant"
	"github.com/r0n9/camkeep/internal/onvif"
)

func TestOnvifEventManagerReferenceCountsLeases(t *testing.T) {
	manager := newOnvifEventManager()
	started := make(chan struct{}, 1)
	stopped := make(chan struct{})
	publishChecks := make(chan func() bool, 1)
	manager.run = func(ctx context.Context, cam constant.Camera, candidate onvif.Candidate, shouldPublishMotion func() bool) {
		started <- struct{}{}
		publishChecks <- shouldPublishMotion
		<-ctx.Done()
		close(stopped)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cam := constant.Camera{ID: "cam1"}
	candidate := onvif.Candidate{ID: "cam1"}

	releaseFirst := manager.acquire(ctx, cam, candidate, "motion-recording", true)
	waitForTestSignal(t, started, "event watcher start")
	shouldPublishMotion := waitForTestValue(t, publishChecks, "publish check callback")
	if !shouldPublishMotion() {
		t.Fatal("expected motion-recording lease to allow publishing motion events")
	}

	releaseSecond := manager.acquire(ctx, cam, candidate, "future-consumer", false)
	if watcherCount, leaseCount := managerCounts(manager, cam.ID); watcherCount != 1 || leaseCount != 2 {
		t.Fatalf("expected one watcher with two leases, got watchers=%d leases=%d", watcherCount, leaseCount)
	}

	releaseFirst()
	if watcherCount, leaseCount := managerCounts(manager, cam.ID); watcherCount != 1 || leaseCount != 1 {
		t.Fatalf("expected one watcher with one lease after first release, got watchers=%d leases=%d", watcherCount, leaseCount)
	}
	if shouldPublishMotion() {
		t.Fatal("expected diagnostic-only lease not to publish motion events")
	}
	select {
	case <-stopped:
		t.Fatal("expected watcher to keep running while one lease remains")
	default:
	}

	releaseSecond()
	if watcherCount, leaseCount := managerCounts(manager, cam.ID); watcherCount != 0 || leaseCount != 0 {
		t.Fatalf("expected watcher to stop after final release, got watchers=%d leases=%d", watcherCount, leaseCount)
	}
	waitForClosedTestSignal(t, stopped, "event watcher stop")
}

func TestOnvifEventManagerDiagnosticLeaseDoesNotPublishMotion(t *testing.T) {
	manager := newOnvifEventManager()
	started := make(chan struct{}, 1)
	publishChecks := make(chan func() bool, 1)
	manager.run = func(ctx context.Context, cam constant.Camera, candidate onvif.Candidate, shouldPublishMotion func() bool) {
		started <- struct{}{}
		publishChecks <- shouldPublishMotion
		<-ctx.Done()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cam := constant.Camera{ID: "cam1"}
	candidate := onvif.Candidate{ID: "cam1"}
	release := manager.acquire(ctx, cam, candidate, "live-overlay", false)
	defer release()
	waitForTestSignal(t, started, "event watcher start")
	shouldPublishMotion := waitForTestValue(t, publishChecks, "publish check callback")
	if shouldPublishMotion() {
		t.Fatal("expected live-overlay lease not to publish motion events")
	}
}

func managerCounts(manager *onvifEventManager, camID string) (int, int) {
	manager.mux.Lock()
	defer manager.mux.Unlock()

	watcher := manager.watchers[camID]
	if watcher == nil {
		return len(manager.watchers), 0
	}
	return len(manager.watchers), len(watcher.leases)
}

func waitForTestSignal(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func waitForTestValue[T any](t *testing.T, ch <-chan T, name string) T {
	t.Helper()

	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		var zero T
		t.Fatalf("timed out waiting for %s", name)
		return zero
	}
}

func waitForClosedTestSignal(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()

	select {
	case <-ch:
	default:
		t.Fatalf("expected %s signal to be closed", name)
	}
}
