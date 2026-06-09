package app

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/r0n9/camkeep/constant"
	"gopkg.in/yaml.v3"
)

// loadOrInitConfig 如果配置文件不存在则生成一个带示例的默认配置
func loadOrInitConfig() constant.Config {
	os.MkdirAll(filepath.Dir(constant.ConfigFilePath), 0755)

	data, err := os.ReadFile(constant.ConfigFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Println("conf.yaml 不存在，自动创建默认模板配置...")

			// 定义默认配置模板
			defaultCfg := constant.Config{
				DailyMerge: constant.DailyMergeConfig{
					Enabled:            false,
					Time:               "03:30",
					MergeMotionRecords: false,
				},
				Cameras: []constant.Camera{
					{
						ID:                         "camkeep",
						Order:                      0,
						StreamURL:                  "rtsp://admin:password@192.168.1.100:554/live",
						MotionURL:                  "",
						RetentionDays:              7,
						SegmentDuration:            600,
						Format:                     constant.DefaultRecordFormat,
						MinSizeKb:                  1024,
						RecordTime:                 "00:00-23:59",
						Mode:                       "normal",
						MotionDetect:               false,
						MotionEventSource:          constant.MotionEventSourceFrameDiff,
						MotionDetectRatioThreshold: 0.01,
					},
				},
			}

			// 将默认配置序列化为 YAML
			out, err := yaml.Marshal(defaultCfg)
			if err != nil {
				log.Printf("生成默认配置失败: %v", err)
				return defaultCfg
			}

			// 可以在文件开头写入一些注释说明（可选）
			header := []byte("# CamKeep 配置文件\n# mode: normal (普通录制), timelapse (延时摄影)\n")
			finalOut := append(header, out...)

			if err := os.WriteFile(constant.ConfigFilePath, finalOut, 0644); err != nil {
				log.Printf("写入配置文件失败: %v", err)
			}

			return defaultCfg
		}
		log.Fatalf("读取配置文件失败: %v", err)
	}

	var c constant.Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		log.Fatalf("解析配置文件失败: %v", err)
	}

	// 启动时如果发现文件里有重复 ID，自动去重
	c = validateAndFixConfig(c)
	return c
}

func parseConfigYAML(yamlBytes []byte) (constant.Config, error) {
	var cfg constant.Config
	if strings.TrimSpace(string(yamlBytes)) == "" {
		return cfg, fmt.Errorf("conf.yaml 不能为空")
	}

	decoder := yaml.NewDecoder(bytes.NewReader(yamlBytes))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("YAML 格式有误: %w", err)
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return cfg, fmt.Errorf("YAML 格式有误: %w", err)
		}
		return cfg, fmt.Errorf("conf.yaml 只能包含一个 YAML 文档")
	}

	if err := validateConfig(cfg); err != nil {
		return cfg, err
	}
	applyConfigDefaults(&cfg)
	return cfg, nil
}

func validateConfig(cfg constant.Config) error {
	if cfg.DailyMerge.Enabled && cfg.DailyMerge.Time == "" {
		return fmt.Errorf("daily_merge.time 不能为空")
	}
	if cfg.DailyMerge.Time != "" {
		if _, err := time.Parse("15:04", cfg.DailyMerge.Time); err != nil {
			return fmt.Errorf("daily_merge.time 必须使用 HH:mm 格式")
		}
	}

	seen := make(map[string]bool)
	for _, cam := range cfg.Cameras {
		if cam.ID == "" {
			return fmt.Errorf("摄像头 ID 不能为空")
		}
		if seen[cam.ID] {
			return fmt.Errorf("发现重复的摄像头 ID: %s", cam.ID)
		}
		seen[cam.ID] = true

		if err := validateCameraConfig(cam); err != nil {
			return fmt.Errorf("摄像头 %s 配置错误: %w", cam.ID, err)
		}
	}
	return nil
}

func validateCameraConfig(cam constant.Camera) error {
	format := strings.TrimSpace(cam.Format)
	if format != "" && format != "ts" && format != "mp4" {
		return fmt.Errorf("format 仅支持 ts 或 mp4")
	}

	mode := strings.TrimSpace(cam.Mode)
	if mode != "" && mode != "normal" && mode != "timelapse" {
		return fmt.Errorf("mode 仅支持 normal 或 timelapse")
	}

	if cam.SegmentDuration < 0 {
		return fmt.Errorf("segment_duration 不能为负数")
	}
	if cam.RetentionDays < -1 {
		return fmt.Errorf("retention_days 不能小于 -1")
	}
	if cam.CaptureInterval < 0 {
		return fmt.Errorf("capture_interval 不能为负数")
	}
	if cam.MinSizeKb < 0 {
		return fmt.Errorf("min_size_kb 不能为负数")
	}
	if cam.Order < 0 {
		return fmt.Errorf("order 不能为负数")
	}
	if cam.MotionDetectRatioThreshold < 0 || cam.MotionDetectRatioThreshold > 1 {
		return fmt.Errorf("motionDetectRatioThreshold 必须在 0 到 1 之间")
	}
	if !constant.ValidMotionEventSource(cam.MotionEventSource) {
		return fmt.Errorf("motion_event_source 仅支持 frame_diff、onvif 或 auto")
	}
	if !constant.ValidMotionEventSource(cam.MotionMarkEventSource) {
		return fmt.Errorf("motion_mark_event_source 仅支持 frame_diff、onvif 或 auto")
	}

	return nil
}

func applyConfigDefaults(cfg *constant.Config) {
	for i := range cfg.Cameras {
		cfg.Cameras[i].Format = normalizedRecordFormat(cfg.Cameras[i].Format)
		cfg.Cameras[i].MotionEventSource = constant.NormalizeMotionEventSource(cfg.Cameras[i].MotionEventSource)
		cfg.Cameras[i].MotionMarkEventSource = constant.NormalizeMotionMarkEventSource(cfg.Cameras[i].MotionMarkEventSource)
	}
}

func normalizedRecordFormat(format string) string {
	format = strings.TrimSpace(format)
	if format == "" {
		return constant.DefaultRecordFormat
	}
	return format
}

// validateAndFixConfig 修复文件读取时的配置 (用于启动时静默去重防崩溃)
func validateAndFixConfig(cfg constant.Config) constant.Config {
	if cfg.DailyMerge.Time == "" {
		cfg.DailyMerge.Time = "03:30"
	}

	seen := make(map[string]bool)
	var uniqueCams []constant.Camera

	for _, cam := range cfg.Cameras {
		if cam.ID == "" {
			log.Println("警告: 发现空 ID 的摄像头配置，已跳过")
			continue
		}
		if seen[cam.ID] {
			log.Printf("警告: 发现重复的摄像头 ID [%s]，已自动去重", cam.ID)
			continue
		}
		seen[cam.ID] = true

		if constant.CameraManagedByGo2rtc(cam) {
			cam.AutoDiscovered = true
		}

		// 预置默认录像策略。如果用户在 conf.yaml 中没写，就走这里的兜底。
		if cam.RetentionDays == 0 {
			cam.RetentionDays = 7
		}
		if cam.SegmentDuration == 0 {
			cam.SegmentDuration = 600
		}
		cam.Format = normalizedRecordFormat(cam.Format)
		if cam.MinSizeKb == 0 {
			cam.MinSizeKb = 1024
		}
		if cam.RecordTime == "" {
			cam.RecordTime = "00:00-23:59"
		}
		if cam.Mode == "" {
			cam.Mode = "normal" // 普通录制模式
		}
		cam.MotionEventSource = constant.NormalizeMotionEventSource(cam.MotionEventSource)
		cam.MotionMarkEventSource = constant.NormalizeMotionMarkEventSource(cam.MotionMarkEventSource)

		uniqueCams = append(uniqueCams, cam)
	}
	cfg.Cameras = uniqueCams
	return cfg
}
