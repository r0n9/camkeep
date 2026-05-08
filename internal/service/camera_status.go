package service

import (
	"sync"
	"time"
)

type CameraStatus struct {
	ID          string    `json:"id"`
	IsRunning   bool      `json:"is_running"`
	StartTime   time.Time `json:"start_time"`
	Mode        string    `json:"mode"`
	StreamState string    `json:"stream_state"` // 流状态: online(在线), offline(断线), idle(按需休眠)
}

var (
	StatusMap = make(map[string]*CameraStatus)
	StatusMux sync.RWMutex
)

// UpdateStatus 更新录像状态
func UpdateStatus(id string, isRunning bool, mode string) {
	StatusMux.Lock()
	defer StatusMux.Unlock()
	if _, exists := StatusMap[id]; !exists {
		StatusMap[id] = &CameraStatus{StreamState: "offline"} // 默认状态
	}
	StatusMap[id].IsRunning = isRunning
	StatusMap[id].Mode = mode
}

func UpdateRecordStatus(id string, isRunning bool) {
	StatusMux.Lock()
	defer StatusMux.Unlock()
	if _, exists := StatusMap[id]; !exists {
		StatusMap[id] = &CameraStatus{StreamState: "offline"} // 默认状态
	}
	StatusMap[id].IsRunning = isRunning
}

// UpdateOnlineStatus 更新实时流状态
func UpdateOnlineStatus(id string, state string) {
	StatusMux.Lock()
	defer StatusMux.Unlock()
	if _, exists := StatusMap[id]; !exists {
		StatusMap[id] = &CameraStatus{}
	}
	StatusMap[id].StreamState = state
}
