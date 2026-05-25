package task

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	EventTypeMotion = "motion"
)

type DetectionEvent struct {
	ID       string
	CameraID string
	Type     string
	Source   string
	At       time.Time
	Snapshot string
	Metadata map[string]string
}

var (
	detectionEventMux sync.RWMutex
	detectionEvents   = make(map[string]map[string]DetectionEvent)
)

func PublishDetectionEvent(event DetectionEvent) {
	event.CameraID = strings.TrimSpace(event.CameraID)
	event.Type = strings.TrimSpace(event.Type)
	if event.CameraID == "" || event.Type == "" {
		return
	}
	if event.At.IsZero() {
		event.At = time.Now()
	}
	if event.ID == "" {
		event.ID = fmt.Sprintf("%s-%s-%d", event.CameraID, event.Type, event.At.UnixNano())
	}

	detectionEventMux.Lock()
	defer detectionEventMux.Unlock()

	eventsByType, exists := detectionEvents[event.CameraID]
	if !exists {
		eventsByType = make(map[string]DetectionEvent)
		detectionEvents[event.CameraID] = eventsByType
	}
	if existing, exists := eventsByType[event.Type]; exists && existing.At.After(event.At) {
		return
	}
	eventsByType[event.Type] = cloneDetectionEvent(event)
}

func RecentDetectionEvent(cameraID string, eventType string, now time.Time, within time.Duration) (DetectionEvent, bool) {
	if within <= 0 {
		return DetectionEvent{}, false
	}
	if now.IsZero() {
		now = time.Now()
	}

	detectionEventMux.RLock()
	defer detectionEventMux.RUnlock()

	eventsByType := detectionEvents[strings.TrimSpace(cameraID)]
	if eventsByType == nil {
		return DetectionEvent{}, false
	}
	event, exists := eventsByType[strings.TrimSpace(eventType)]
	if !exists || event.At.IsZero() || now.Sub(event.At) >= within {
		return DetectionEvent{}, false
	}
	return cloneDetectionEvent(event), true
}

func ResetCameraDetectionEvents(cameraID string) {
	detectionEventMux.Lock()
	delete(detectionEvents, strings.TrimSpace(cameraID))
	detectionEventMux.Unlock()
}

func cloneDetectionEvent(event DetectionEvent) DetectionEvent {
	if event.Metadata == nil {
		return event
	}
	metadata := make(map[string]string, len(event.Metadata))
	for key, value := range event.Metadata {
		metadata[key] = value
	}
	event.Metadata = metadata
	return event
}
