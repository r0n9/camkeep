package app

import (
	"testing"
	"time"
)

func TestSessionTrackerTrackLoginPrunesExpiredSessions(t *testing.T) {
	tracker := newSessionTracker()
	tracker.pruneInterval = 0
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.Local)
	user := currentUser{ID: "user-1"}

	tracker.sessions[sessionKey("expired")] = trackedSession{
		UserID:     user.ID,
		LastSeenAt: now.Add(-time.Hour),
		ExpiresAt:  now.Add(-time.Minute),
	}

	tracker.trackLogin("fresh", user, "203.0.113.10", now, now.Add(time.Hour))

	if _, ok := tracker.sessions[sessionKey("expired")]; ok {
		t.Fatal("expected expired session to be pruned during login tracking")
	}
	if _, ok := tracker.sessions[sessionKey("fresh")]; !ok {
		t.Fatal("expected fresh login session to be tracked")
	}
}

func TestSessionTrackerTouchPrunesExpiredSessions(t *testing.T) {
	tracker := newSessionTracker()
	tracker.pruneInterval = 0
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.Local)
	user := currentUser{ID: "user-1"}

	tracker.sessions[sessionKey("expired")] = trackedSession{
		UserID:     user.ID,
		LastSeenAt: now.Add(-time.Hour),
		ExpiresAt:  now.Add(-time.Minute),
	}
	tracker.sessions[sessionKey("active")] = trackedSession{
		UserID:     user.ID,
		LoginAt:    now.Add(-time.Minute),
		LastSeenAt: now.Add(-time.Minute),
		ExpiresAt:  now.Add(time.Hour),
	}

	tracker.touch("active", user, "203.0.113.10", now, now.Add(time.Hour))

	if _, ok := tracker.sessions[sessionKey("expired")]; ok {
		t.Fatal("expected expired session to be pruned during touch")
	}
	active := tracker.sessions[sessionKey("active")]
	if active.UserID != user.ID || !active.LastSeenAt.Equal(now) {
		t.Fatalf("expected active session to be refreshed, got %+v", active)
	}
}
