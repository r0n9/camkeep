package service

import (
	"sync"
	"time"
)

type CameraStatus struct {
	ID        string    `json:"id"`
	IsRunning bool      `json:"is_running"`
	StartTime time.Time `json:"start_time"`
	Mode      string    `json:"mode"`
}

var (
	StatusMap = make(map[string]*CameraStatus)
	StatusMux sync.RWMutex
)

// UpdateStatus 开放给 task 包调用，实时更新摄像头状态
func UpdateStatus(id string, running bool, mode string) {
	StatusMux.Lock()
	defer StatusMux.Unlock()
	StatusMap[id] = &CameraStatus{
		ID:        id,
		IsRunning: running,
		StartTime: time.Now(),
		Mode:      mode,
	}
}
