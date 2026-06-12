package app

import (
	"crypto/sha256"
	"encoding/base64"
	"sort"
	"sync"
	"time"
)

const (
	activeSessionWindow         = 2 * time.Minute
	sessionTrackerPruneInterval = time.Minute
)

type userSessionView struct {
	IP         string    `json:"ip"`
	LoginAt    time.Time `json:"login_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	Current    bool      `json:"current"`
}

type trackedSession struct {
	UserID     string
	IP         string
	LoginAt    time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

type sessionTracker struct {
	mux           sync.Mutex
	sessions      map[string]trackedSession
	activeWindow  time.Duration
	pruneInterval time.Duration
	lastPrunedAt  time.Time
}

func newSessionTracker() *sessionTracker {
	return &sessionTracker{
		sessions:      make(map[string]trackedSession),
		activeWindow:  activeSessionWindow,
		pruneInterval: sessionTrackerPruneInterval,
	}
}

func (t *sessionTracker) trackLogin(token string, user currentUser, ip string, now, expiresAt time.Time) {
	if t == nil || token == "" || user.ID == "" {
		return
	}

	t.mux.Lock()
	defer t.mux.Unlock()
	t.pruneExpiredIfDueLocked(now)
	t.sessions[sessionKey(token)] = trackedSession{
		UserID:     user.ID,
		IP:         ip,
		LoginAt:    now,
		LastSeenAt: now,
		ExpiresAt:  expiresAt,
	}
}

func (t *sessionTracker) touch(token string, user currentUser, ip string, now, expiresAt time.Time) {
	if t == nil || token == "" || user.ID == "" {
		return
	}

	key := sessionKey(token)
	t.mux.Lock()
	defer t.mux.Unlock()
	t.pruneExpiredIfDueLocked(now)
	session := t.sessions[key]
	if session.UserID == "" {
		session.UserID = user.ID
		session.LoginAt = now
	}
	session.IP = ip
	session.LastSeenAt = now
	session.ExpiresAt = expiresAt
	t.sessions[key] = session
}

func (t *sessionTracker) remove(token string) {
	if t == nil || token == "" {
		return
	}

	t.mux.Lock()
	defer t.mux.Unlock()
	delete(t.sessions, sessionKey(token))
}

func (t *sessionTracker) removeByUserID(userID string) {
	if t == nil || userID == "" {
		return
	}

	t.mux.Lock()
	defer t.mux.Unlock()
	for key, session := range t.sessions {
		if session.UserID == userID {
			delete(t.sessions, key)
		}
	}
}

func (t *sessionTracker) activeSessionsByUser(now time.Time, currentToken string) map[string][]userSessionView {
	result := map[string][]userSessionView{}
	if t == nil {
		return result
	}

	currentKey := sessionKey(currentToken)
	t.mux.Lock()
	defer t.mux.Unlock()
	for key, session := range t.sessions {
		if !session.isActive(now, t.activeWindow) {
			delete(t.sessions, key)
			continue
		}
		result[session.UserID] = append(result[session.UserID], userSessionView{
			IP:         session.IP,
			LoginAt:    session.LoginAt,
			LastSeenAt: session.LastSeenAt,
			Current:    currentKey != "" && key == currentKey,
		})
	}
	for userID := range result {
		sort.Slice(result[userID], func(i, j int) bool {
			if result[userID][i].Current != result[userID][j].Current {
				return result[userID][i].Current
			}
			return result[userID][i].LastSeenAt.After(result[userID][j].LastSeenAt)
		})
	}
	return result
}

func (t *sessionTracker) pruneExpiredIfDueLocked(now time.Time) {
	if t.pruneInterval > 0 && !t.lastPrunedAt.IsZero() && now.Before(t.lastPrunedAt.Add(t.pruneInterval)) {
		return
	}
	t.pruneExpiredLocked(now)
	t.lastPrunedAt = now
}

func (t *sessionTracker) pruneExpiredLocked(now time.Time) {
	for key, session := range t.sessions {
		if session.isExpired(now) {
			delete(t.sessions, key)
		}
	}
}

func (s trackedSession) isActive(now time.Time, activeWindow time.Duration) bool {
	if s.isExpired(now) {
		return false
	}
	return now.Sub(s.LastSeenAt) <= activeWindow
}

func (s trackedSession) isExpired(now time.Time) bool {
	return s.UserID == "" || !s.ExpiresAt.After(now)
}

func sessionKey(token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
