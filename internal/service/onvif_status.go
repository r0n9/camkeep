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
)

type OnvifStatus struct {
	ID               string    `json:"id"`
	Enabled          bool      `json:"enabled"`
	SourceType       string    `json:"source_type"`
	SourceURL        string    `json:"source_url"`
	Endpoint         string    `json:"endpoint"`
	Username         string    `json:"username,omitempty"`
	ManagedByGo2rtc  bool      `json:"managed_by_go2rtc"`
	CapabilityState  string    `json:"capability_state"`
	PTZState         string    `json:"ptz_state"`
	ImagingState     string    `json:"imaging_state"`
	EventState       string    `json:"event_state"`
	DeviceXAddr      string    `json:"device_xaddr,omitempty"`
	MediaXAddr       string    `json:"media_xaddr,omitempty"`
	PTZXAddr         string    `json:"ptz_xaddr,omitempty"`
	ImagingXAddr     string    `json:"imaging_xaddr,omitempty"`
	EventXAddr       string    `json:"event_xaddr,omitempty"`
	PullPointSupport bool      `json:"pull_point_support"`
	ProfileToken     string    `json:"profile_token,omitempty"`
	ProfileName      string    `json:"profile_name,omitempty"`
	VideoSourceToken string    `json:"video_source_token,omitempty"`
	LastError        string    `json:"last_error,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`

	sourceFingerprint string
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
			ID:                candidate.ID,
			Enabled:           true,
			SourceType:        candidate.SourceType,
			SourceURL:         onvif.MaskSourceURL(candidate.SourceURL),
			Endpoint:          candidate.Endpoint,
			Username:          candidate.Username,
			ManagedByGo2rtc:   candidate.ManagedByGo2rtc,
			CapabilityState:   OnvifStateNotProbed,
			PTZState:          OnvifStateNotProbed,
			ImagingState:      OnvifStateNotProbed,
			EventState:        OnvifStateNotProbed,
			UpdatedAt:         now,
			sourceFingerprint: onvif.SourceFingerprint(candidate.SourceURL),
		}

		if prev, ok := previous[candidate.ID]; ok && prev.sourceFingerprint == status.sourceFingerprint && prev.Endpoint == status.Endpoint {
			status.CapabilityState = keepOnvifState(prev.CapabilityState)
			status.PTZState = keepOnvifState(prev.PTZState)
			status.ImagingState = keepOnvifState(prev.ImagingState)
			status.EventState = keepOnvifState(prev.EventState)
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
		}
		status.UpdatedAt = time.Now()
	})
}

func ListOnvifStatuses() []OnvifStatus {
	OnvifStatusMux.RLock()
	statuses := make([]OnvifStatus, 0, len(onvifStatusMap))
	for _, status := range onvifStatusMap {
		statuses = append(statuses, *status)
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
	return *status, true
}

func keepOnvifState(state string) string {
	if state == "" {
		return OnvifStateNotProbed
	}
	return state
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
