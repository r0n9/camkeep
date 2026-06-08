package app

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/r0n9/camkeep/constant"
	"github.com/r0n9/camkeep/internal/onvif"
	"github.com/r0n9/camkeep/internal/task"
)

const onvifEventSummaryLeaseTTL = 15 * time.Second

var onvifEventSummaryLeases = newOnvifEventSummaryLeaseRegistry()

type onvifEventSummaryLeaseRegistry struct {
	mux    sync.Mutex
	leases map[string]*onvifEventSummaryLease
}

type onvifEventSummaryLease struct {
	cancel    context.CancelFunc
	release   func()
	timer     *time.Timer
	expiresAt time.Time
}

func newOnvifEventSummaryLeaseRegistry() *onvifEventSummaryLeaseRegistry {
	return &onvifEventSummaryLeaseRegistry{
		leases: make(map[string]*onvifEventSummaryLease),
	}
}

func (r *onvifEventSummaryLeaseRegistry) refresh(cam constant.Camera, candidate onvif.Candidate, clientID string, ttl time.Duration) time.Time {
	if ttl <= 0 {
		ttl = onvifEventSummaryLeaseTTL
	}
	clientID = normalizeOnvifEventSummaryClientID(clientID)
	if clientID == "" {
		return time.Time{}
	}

	key := onvifEventSummaryLeaseKey(cam.ID, clientID)
	expiresAt := time.Now().Add(ttl)

	r.mux.Lock()
	if r.leases == nil {
		r.leases = make(map[string]*onvifEventSummaryLease)
	}
	if lease := r.leases[key]; lease != nil {
		lease.expiresAt = expiresAt
		lease.timer.Reset(ttl)
		r.mux.Unlock()
		return expiresAt
	}

	parent := ctxGlobal
	if parent == nil {
		parent = context.Background()
	}
	leaseCtx, cancel := context.WithCancel(parent)
	releaseTask := task.RequireOnvifEventStream(leaseCtx, cam, candidate, "live-event-overlay")
	lease := &onvifEventSummaryLease{
		cancel:    cancel,
		release:   releaseTask,
		expiresAt: expiresAt,
	}
	lease.timer = time.AfterFunc(ttl, func() {
		r.expireByKey(key)
	})
	r.leases[key] = lease
	r.mux.Unlock()

	return expiresAt
}

func (r *onvifEventSummaryLeaseRegistry) release(camID, clientID string) {
	clientID = normalizeOnvifEventSummaryClientID(clientID)
	if clientID == "" {
		return
	}
	r.releaseByKey(onvifEventSummaryLeaseKey(camID, clientID))
}

func (r *onvifEventSummaryLeaseRegistry) releaseByKey(key string) {
	r.mux.Lock()
	lease := r.leases[key]
	if lease == nil {
		r.mux.Unlock()
		return
	}
	delete(r.leases, key)
	if lease.timer != nil {
		lease.timer.Stop()
	}
	r.mux.Unlock()

	lease.cancel()
	lease.release()
}

func (r *onvifEventSummaryLeaseRegistry) expireByKey(key string) {
	r.mux.Lock()
	lease := r.leases[key]
	if lease == nil {
		r.mux.Unlock()
		return
	}
	if now := time.Now(); now.Before(lease.expiresAt) {
		lease.timer.Reset(lease.expiresAt.Sub(now))
		r.mux.Unlock()
		return
	}
	delete(r.leases, key)
	r.mux.Unlock()

	lease.cancel()
	lease.release()
}

func onvifEventSummaryLeaseKey(camID, clientID string) string {
	return strings.TrimSpace(camID) + "\x00" + normalizeOnvifEventSummaryClientID(clientID)
}

func normalizeOnvifEventSummaryClientID(clientID string) string {
	clientID = strings.TrimSpace(clientID)
	if len(clientID) > 128 {
		clientID = clientID[:128]
	}
	return clientID
}
