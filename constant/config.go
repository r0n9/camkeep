package constant

import "sync"

var ConfigMux sync.RWMutex

type Camera struct {
	ID              string `yaml:"id"`
	RTSPUrl         string `yaml:"rtsp_url"`
	MotionURL       string `yaml:"motion_url"` // 可选：仅用于动检识别的流地址，录像仍使用 rtsp_url 对应主码流
	RetentionDays   int    `yaml:"retention_days"`
	SegmentDuration int    `yaml:"segment_duration"`
	Format          string `yaml:"format"`
	MinSizeKb       int64  `yaml:"min_size_kb"`
	RecordTime      string `yaml:"record_time"`
	Mode            string `yaml:"mode"`             // 模式: "normal" 或 "timelapse"，留空默认为 normal
	CaptureInterval int    `yaml:"capture_interval"` // 抓拍间隔(秒)，例如 5 表示每5秒抓一帧
	MotionDetect    bool   `yaml:"motion_detect"`    // 是否开启动检录制，仅 normal 模式生效
	// motionDetectRatioThreshold: 判定发生运动的变化像素比例阈值，仅 motion_detect=true 时生效。
	MotionDetectRatioThreshold float64 `yaml:"motionDetectRatioThreshold"`

	AutoDiscovered bool `yaml:"auto_discovered"` // 标识这个流是手动配置的，还是从 go2rtc 自动发现的
}

type DailyMergeConfig struct {
	Enabled bool   `yaml:"enabled"`
	Time    string `yaml:"time"`
}

// Config 对应 yaml 配置文件
type Config struct {
	DailyMerge DailyMergeConfig `yaml:"daily_merge"`
	Cameras    []Camera         `yaml:"cameras"`
}
