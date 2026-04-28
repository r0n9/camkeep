package constant

import "sync"

var ConfigMux sync.RWMutex

type Camera struct {
	ID              string `yaml:"id"`
	RTSPUrl         string `yaml:"rtsp_url"`
	RetentionDays   int    `yaml:"retention_days"`
	SegmentDuration int    `yaml:"segment_duration"`
	Format          string `yaml:"format"`
	MinSizeKb       int64  `yaml:"min_size_kb"`
	RecordTime      string `yaml:"record_time"`
	Mode            string `yaml:"mode"`             // 模式: "normal" 或 "timelapse"，留空默认为 normal
	CaptureInterval int    `yaml:"capture_interval"` // 抓拍间隔(秒)，例如 5 表示每5秒抓一帧
}

// Config 对应 yaml 配置文件
type Config struct {
	Cameras []Camera `yaml:"cameras"`
}
