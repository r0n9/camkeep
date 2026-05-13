package service

import (
	"sync"
	"time"
)

type CameraStatus struct {
	ID          string    `json:"id"`
	IsRunning   bool      `json:"is_running"`
	RecordState string    `json:"record_state"` // 录像状态: idle, recording, motion_detecting, motion_recording
	StartTime   time.Time `json:"start_time"`
	Mode        string    `json:"mode"`
	RecordTime  string    `json:"record_time"`
	StreamState string    `json:"stream_state"` // 流状态: online(在线), offline(断线), idle(按需休眠)
}

const (
	RecordStateIdle            = "idle"
	RecordStateRecording       = "recording"
	RecordStateMotionDetecting = "motion_detecting"
	RecordStateMotionRecording = "motion_recording"
)

var (
	StatusMap = make(map[string]*CameraStatus)
	StatusMux sync.RWMutex
)

// UpdateStatus 更新录像状态
func UpdateStatus(id string, isRunning bool, mode string, recordTime string) {
	StatusMux.Lock()
	defer StatusMux.Unlock()
	status := ensureCameraStatus(id)
	status.IsRunning = isRunning
	status.RecordState = recordStateFromRunning(isRunning)
	status.Mode = mode
	status.RecordTime = recordTime
}

func UpdateRecordStatus(id string, isRunning bool) {
	StatusMux.Lock()
	defer StatusMux.Unlock()
	status := ensureCameraStatus(id)
	status.IsRunning = isRunning
	status.RecordState = recordStateFromRunning(isRunning)
}

func UpdateRecordState(id string, recordState string, mode string, recordTime string) {
	StatusMux.Lock()
	defer StatusMux.Unlock()
	status := ensureCameraStatus(id)
	status.RecordState = normalizeRecordState(recordState)
	status.IsRunning = status.RecordState != RecordStateIdle
	status.Mode = mode
	status.RecordTime = recordTime
}

// UpdateOnlineStatus 更新实时流状态
func UpdateOnlineStatus(id string, state string) {
	StatusMux.Lock()
	defer StatusMux.Unlock()
	status := ensureCameraStatus(id)
	status.StreamState = state
}

func ensureCameraStatus(id string) *CameraStatus {
	if _, exists := StatusMap[id]; !exists {
		StatusMap[id] = &CameraStatus{
			StreamState: "offline",
			RecordState: RecordStateIdle,
		}
	}
	if StatusMap[id].RecordState == "" {
		StatusMap[id].RecordState = recordStateFromRunning(StatusMap[id].IsRunning)
	}
	return StatusMap[id]
}

func recordStateFromRunning(isRunning bool) string {
	if isRunning {
		return RecordStateRecording
	}
	return RecordStateIdle
}

func normalizeRecordState(recordState string) string {
	switch recordState {
	case RecordStateRecording, RecordStateMotionDetecting, RecordStateMotionRecording:
		return recordState
	default:
		return RecordStateIdle
	}
}
