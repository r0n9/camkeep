package service

import (
	"sort"
	"sync"
	"time"

	"github.com/r0n9/camkeep/internal/onvif"
)

const (
	OnvifStateNotProbed   = "not_probed"
	OnvifStateProbing     = "probing"
	OnvifStateAvailable   = "available"
	OnvifStateUnavailable = "unavailable"
	OnvifStateError       = "error"

	OnvifEventListenerInactive  = "inactive"
	OnvifEventListenerStarting  = "starting"
	OnvifEventListenerListening = "listening"
	OnvifEventListenerError     = "error"

	OnvifEventSourceHealthWindow = 90 * time.Second
)

type OnvifStatus struct {
	ID                     string              `json:"id"`
	Enabled                bool                `json:"enabled"`
	SourceType             string              `json:"source_type"`
	SourceURL              string              `json:"source_url"`
	Endpoint               string              `json:"endpoint"`
	Username               string              `json:"username,omitempty"`
	ManagedByGo2rtc        bool                `json:"managed_by_go2rtc"`
	CapabilityState        string              `json:"capability_state"`
	PTZState               string              `json:"ptz_state"`
	ImagingState           string              `json:"imaging_state"`
	EventState             string              `json:"event_state"`
	EventListenerState     string              `json:"event_listener_state"`
	DeviceXAddr            string              `json:"device_xaddr,omitempty"`
	MediaXAddr             string              `json:"media_xaddr,omitempty"`
	PTZXAddr               string              `json:"ptz_xaddr,omitempty"`
	ImagingXAddr           string              `json:"imaging_xaddr,omitempty"`
	EventXAddr             string              `json:"event_xaddr,omitempty"`
	PullPointSupport       bool                `json:"pull_point_support"`
	ProfileToken           string              `json:"profile_token,omitempty"`
	ProfileName            string              `json:"profile_name,omitempty"`
	VideoSourceToken       string              `json:"video_source_token,omitempty"`
	LastError              string              `json:"last_error,omitempty"`
	EventListenerLastError string              `json:"event_listener_last_error,omitempty"`
	EventPullLastSuccessAt time.Time           `json:"event_pull_last_success_at,omitempty"`
	MotionEventVerified    bool                `json:"motion_event_verified"`
	LastEvent              *OnvifEventSnapshot `json:"last_event,omitempty"`
	UpdatedAt              time.Time           `json:"updated_at"`

	sourceFingerprint string
}

type OnvifEventSnapshot struct {
	Topic           string    `json:"topic"`
	Operation       string    `json:"operation,omitempty"`
	At              time.Time `json:"at"`
	ReceivedAt      time.Time `json:"received_at"`
	ProducerAddress string    `json:"producer_address,omitempty"`
	Source          string    `json:"source,omitempty"`
	Key             string    `json:"key,omitempty"`
	Data            string    `json:"data,omitempty"`
	Motion          bool      `json:"motion"`
	MotionTopic     bool      `json:"motion_topic"`
}

var (
	OnvifStatusMux sync.RWMutex
	onvifStatusMap = make(map[string]*OnvifStatus)
)

func ReplaceOnvifCandidates(candidates []onvif.Candidate) {
	now := time.Now()
	next := make(map[string]*OnvifStatus, len(candidates))

	OnvifStatusMux.RLock()
	previous := make(map[string]OnvifStatus, len(onvifStatusMap))
	for id, status := range onvifStatusMap {
		previous[id] = *status
	}
	OnvifStatusMux.RUnlock()

	for _, candidate := range candidates {
		status := OnvifStatus{
			ID:                 candidate.ID,
			Enabled:            true,
			SourceType:         candidate.SourceType,
			SourceURL:          onvif.MaskSourceURL(candidate.SourceURL),
			Endpoint:           candidate.Endpoint,
			Username:           candidate.Username,
			ManagedByGo2rtc:    candidate.ManagedByGo2rtc,
			CapabilityState:    OnvifStateNotProbed,
			PTZState:           OnvifStateNotProbed,
			ImagingState:       OnvifStateNotProbed,
			EventState:         OnvifStateNotProbed,
			EventListenerState: OnvifEventListenerInactive,
			UpdatedAt:          now,
			sourceFingerprint:  onvif.SourceFingerprint(candidate.SourceURL),
		}

		if prev, ok := previous[candidate.ID]; ok && prev.sourceFingerprint == status.sourceFingerprint && prev.Endpoint == status.Endpoint {
			status.CapabilityState = keepOnvifState(prev.CapabilityState)
			status.PTZState = keepOnvifState(prev.PTZState)
			status.ImagingState = keepOnvifState(prev.ImagingState)
			status.EventState = keepOnvifState(prev.EventState)
			status.EventListenerState = keepOnvifEventListenerState(prev.EventListenerState)
			status.DeviceXAddr = prev.DeviceXAddr
			status.MediaXAddr = prev.MediaXAddr
			status.PTZXAddr = prev.PTZXAddr
			status.ImagingXAddr = prev.ImagingXAddr
			status.EventXAddr = prev.EventXAddr
			status.PullPointSupport = prev.PullPointSupport
			status.ProfileToken = prev.ProfileToken
			status.ProfileName = prev.ProfileName
			status.VideoSourceToken = prev.VideoSourceToken
			status.LastError = prev.LastError
			status.EventListenerLastError = prev.EventListenerLastError
			status.EventPullLastSuccessAt = prev.EventPullLastSuccessAt
			status.MotionEventVerified = prev.MotionEventVerified
			status.LastEvent = cloneOnvifEventSnapshot(prev.LastEvent)
		}
		next[candidate.ID] = &status
	}

	OnvifStatusMux.Lock()
	onvifStatusMap = next
	OnvifStatusMux.Unlock()
}

func MarkOnvifProbeStarted(candidate onvif.Candidate) {
	updateOnvifStatusForCandidate(candidate, func(status *OnvifStatus) {
		status.CapabilityState = OnvifStateProbing
		status.PTZState = OnvifStateProbing
		status.ImagingState = OnvifStateProbing
		status.EventState = OnvifStateProbing
		status.LastError = ""
		status.EventListenerLastError = ""
		status.UpdatedAt = time.Now()
	})
}

func UpdateOnvifProbeResult(candidate onvif.Candidate, caps onvif.Capabilities) {
	updateOnvifStatusForCandidate(candidate, func(status *OnvifStatus) {
		status.CapabilityState = OnvifStateAvailable
		status.PTZState = ptzAvailabilityState(caps.PTZXAddr, caps.ProfileToken)
		status.ImagingState = imagingAvailabilityState(caps.ImagingXAddr, caps.VideoSourceToken)
		status.EventState = availabilityState(caps.EventXAddr)
		status.DeviceXAddr = caps.DeviceXAddr
		status.MediaXAddr = caps.MediaXAddr
		status.PTZXAddr = caps.PTZXAddr
		status.ImagingXAddr = caps.ImagingXAddr
		status.EventXAddr = caps.EventXAddr
		status.PullPointSupport = caps.PullPointSupport
		status.ProfileToken = caps.ProfileToken
		status.ProfileName = caps.ProfileName
		status.VideoSourceToken = caps.VideoSourceToken
		status.LastError = ""
		if status.EventState != OnvifStateAvailable || !caps.PullPointSupport {
			status.EventListenerState = OnvifEventListenerInactive
			status.EventPullLastSuccessAt = time.Time{}
		}
		status.EventListenerLastError = ""
		status.UpdatedAt = time.Now()
	})
}

func UpdateOnvifProbeError(candidate onvif.Candidate, err error) {
	updateOnvifStatusForCandidate(candidate, func(status *OnvifStatus) {
		status.CapabilityState = OnvifStateError
		status.PTZState = OnvifStateError
		status.ImagingState = OnvifStateError
		status.EventState = OnvifStateError
		if err != nil {
			status.LastError = err.Error()
			status.EventListenerLastError = err.Error()
		}
		status.EventListenerState = OnvifEventListenerError
		status.EventPullLastSuccessAt = time.Time{}
		status.UpdatedAt = time.Now()
	})
}

func UpdateOnvifEventListenerStarting(id string) {
	updateOnvifStatus(id, func(status *OnvifStatus) {
		status.EventListenerState = OnvifEventListenerStarting
		status.EventListenerLastError = ""
	})
}

func UpdateOnvifEventListenerListening(id string, at time.Time) {
	updateOnvifStatus(id, func(status *OnvifStatus) {
		status.EventListenerState = OnvifEventListenerListening
		status.EventListenerLastError = ""
		if !at.IsZero() {
			status.EventPullLastSuccessAt = at
		}
	})
}

func UpdateOnvifEventListenerError(id string, err error) {
	updateOnvifStatus(id, func(status *OnvifStatus) {
		status.EventListenerState = OnvifEventListenerError
		status.EventPullLastSuccessAt = time.Time{}
		if err != nil {
			status.EventListenerLastError = err.Error()
		}
	})
}

func UpdateOnvifEventListenerInactive(id string) {
	updateOnvifStatus(id, func(status *OnvifStatus) {
		status.EventListenerState = OnvifEventListenerInactive
		status.EventPullLastSuccessAt = time.Time{}
	})
}

func UpdateOnvifLastEvent(id string, event OnvifEventSnapshot) {
	if event.ReceivedAt.IsZero() {
		event.ReceivedAt = time.Now()
	}
	if event.At.IsZero() {
		event.At = event.ReceivedAt
	}

	OnvifStatusMux.Lock()
	defer OnvifStatusMux.Unlock()

	status, ok := onvifStatusMap[id]
	if !ok {
		return
	}
	if status.LastEvent != nil && status.LastEvent.ReceivedAt.After(event.ReceivedAt) {
		return
	}
	status.LastEvent = cloneOnvifEventSnapshot(&event)
	if event.MotionTopic || event.Motion {
		status.MotionEventVerified = true
	}
}

func ListOnvifStatuses() []OnvifStatus {
	OnvifStatusMux.RLock()
	statuses := make([]OnvifStatus, 0, len(onvifStatusMap))
	for _, status := range onvifStatusMap {
		statuses = append(statuses, cloneOnvifStatus(*status))
	}
	OnvifStatusMux.RUnlock()

	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].ID < statuses[j].ID
	})
	return statuses
}

func GetOnvifStatus(id string) (OnvifStatus, bool) {
	OnvifStatusMux.RLock()
	defer OnvifStatusMux.RUnlock()

	status, ok := onvifStatusMap[id]
	if !ok {
		return OnvifStatus{}, false
	}
	return cloneOnvifStatus(*status), true
}

func cloneOnvifStatus(status OnvifStatus) OnvifStatus {
	status.LastEvent = cloneOnvifEventSnapshot(status.LastEvent)
	return status
}

func cloneOnvifEventSnapshot(event *OnvifEventSnapshot) *OnvifEventSnapshot {
	if event == nil {
		return nil
	}
	clone := *event
	return &clone
}

func OnvifEventSourceUsable(id string, now time.Time) bool {
	if now.IsZero() {
		now = time.Now()
	}

	status, ok := GetOnvifStatus(id)
	if !ok {
		return false
	}
	if !status.Enabled || status.EventState != OnvifStateAvailable || status.EventXAddr == "" || !status.PullPointSupport {
		return false
	}
	if status.EventListenerState != OnvifEventListenerListening || status.EventPullLastSuccessAt.IsZero() {
		return false
	}
	return now.Sub(status.EventPullLastSuccessAt) <= OnvifEventSourceHealthWindow
}

func keepOnvifState(state string) string {
	if state == "" {
		return OnvifStateNotProbed
	}
	return state
}

func keepOnvifEventListenerState(state string) string {
	switch state {
	case OnvifEventListenerStarting, OnvifEventListenerListening, OnvifEventListenerError:
		return state
	default:
		return OnvifEventListenerInactive
	}
}

func updateOnvifStatus(id string, update func(*OnvifStatus)) {
	OnvifStatusMux.Lock()
	defer OnvifStatusMux.Unlock()

	status, ok := onvifStatusMap[id]
	if !ok {
		return
	}
	update(status)
}

func updateOnvifStatusForCandidate(candidate onvif.Candidate, update func(*OnvifStatus)) {
	sourceFingerprint := onvif.SourceFingerprint(candidate.SourceURL)

	OnvifStatusMux.Lock()
	defer OnvifStatusMux.Unlock()

	status, ok := onvifStatusMap[candidate.ID]
	if !ok || status.sourceFingerprint != sourceFingerprint || status.Endpoint != candidate.Endpoint {
		return
	}
	update(status)
}

func availabilityState(xaddr string) string {
	if xaddr == "" {
		return OnvifStateUnavailable
	}
	return OnvifStateAvailable
}

func ptzAvailabilityState(ptzXAddr, profileToken string) string {
	if ptzXAddr == "" || profileToken == "" {
		return OnvifStateUnavailable
	}
	return OnvifStateAvailable
}

func imagingAvailabilityState(imagingXAddr, videoSourceToken string) string {
	if imagingXAddr == "" || videoSourceToken == "" {
		return OnvifStateUnavailable
	}
	return OnvifStateAvailable
}
