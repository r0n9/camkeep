package task

import (
	"context"
	"log"
	"sync"

	"github.com/r0n9/camkeep/constant"
	"github.com/r0n9/camkeep/internal/onvif"
)

var defaultOnvifEventManager = newOnvifEventManager()

type onvifEventManager struct {
	mux         sync.Mutex
	watchers    map[string]*onvifEventWatcher
	nextLeaseID uint64
	run         func(context.Context, constant.Camera, onvif.Candidate, func() bool)
}

type onvifEventWatcher struct {
	cancel context.CancelFunc
	done   chan struct{}
	leases map[uint64]onvifEventLease
}

type onvifEventLease struct {
	reason        string
	publishMotion bool
}

func newOnvifEventManager() *onvifEventManager {
	return &onvifEventManager{
		watchers: make(map[string]*onvifEventWatcher),
		run:      runOnvifEventWatcher,
	}
}

func RequireOnvifMotionEvents(ctx context.Context, cam constant.Camera, candidate onvif.Candidate, reason string) func() {
	return defaultOnvifEventManager.acquire(ctx, cam, candidate, reason, true)
}

func RequireOnvifEventStream(ctx context.Context, cam constant.Camera, candidate onvif.Candidate, reason string) func() {
	return defaultOnvifEventManager.acquire(ctx, cam, candidate, reason, false)
}

func (m *onvifEventManager) acquire(ctx context.Context, cam constant.Camera, candidate onvif.Candidate, reason string, publishMotion bool) func() {
	if ctx == nil {
		ctx = context.Background()
	}
	if reason == "" {
		reason = "unspecified"
	}

	m.mux.Lock()
	if m.watchers == nil {
		m.watchers = make(map[string]*onvifEventWatcher)
	}
	watcher := m.watchers[cam.ID]
	if watcher == nil {
		watcherCtx, cancel := context.WithCancel(context.Background())
		watcher = &onvifEventWatcher{
			cancel: cancel,
			done:   make(chan struct{}),
			leases: make(map[uint64]onvifEventLease),
		}
		m.watchers[cam.ID] = watcher

		run := m.run
		if run == nil {
			run = runOnvifEventWatcher
		}
		log.Printf("[%s] ONVIF PullPoint 事件源已启动: reason=%s", cam.ID, reason)
		go func() {
			defer close(watcher.done)
			run(watcherCtx, cam, candidate, func() bool {
				return m.shouldPublishMotion(cam.ID)
			})
			m.removeWatcherIfCurrent(cam.ID, watcher)
		}()
	}

	leaseID := m.nextLeaseID
	m.nextLeaseID++
	watcher.leases[leaseID] = onvifEventLease{
		reason:        reason,
		publishMotion: publishMotion,
	}
	m.mux.Unlock()

	var once sync.Once
	releaseDone := make(chan struct{})
	release := func() {
		once.Do(func() {
			m.release(cam.ID, leaseID, reason)
			close(releaseDone)
		})
	}
	go func() {
		select {
		case <-ctx.Done():
			release()
		case <-releaseDone:
		}
	}()
	return release
}

func (m *onvifEventManager) release(camID string, leaseID uint64, reason string) {
	m.mux.Lock()
	watcher := m.watchers[camID]
	if watcher == nil {
		m.mux.Unlock()
		return
	}
	delete(watcher.leases, leaseID)
	if len(watcher.leases) > 0 {
		m.mux.Unlock()
		return
	}
	delete(m.watchers, camID)
	cancel := watcher.cancel
	done := watcher.done
	m.mux.Unlock()

	log.Printf("[%s] ONVIF PullPoint 事件源不再需要，正在停止: reason=%s", camID, reason)
	cancel()
	<-done
}

func (m *onvifEventManager) shouldPublishMotion(camID string) bool {
	m.mux.Lock()
	defer m.mux.Unlock()

	watcher := m.watchers[camID]
	if watcher == nil {
		return false
	}
	for _, lease := range watcher.leases {
		if lease.publishMotion {
			return true
		}
	}
	return false
}

func (m *onvifEventManager) removeWatcherIfCurrent(camID string, watcher *onvifEventWatcher) {
	m.mux.Lock()
	if m.watchers[camID] == watcher {
		delete(m.watchers, camID)
	}
	m.mux.Unlock()
}
